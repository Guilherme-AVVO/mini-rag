// cmd/pdfprep: prepara PDFs para a ingestão do Mini RAG.
//
// Transforma kb/pdf/*.pdf em dois artefatos por documento:
//   - kb/raw/<slug>.txt   → texto cru do pdftotext, páginas juntadas com \f
//   - kb/clean/<slug>.txt → texto limpo (textclean), \f preservado
//
// O raw existe de propósito: é o "antes" para diff contra o clean — assim dá
// para auditar exatamente o que a limpeza removeu de cada documento antes de
// rodar `go run ./cmd/ingest` (que lê kb/clean/*.txt).
//
// Uso: go run ./cmd/pdfprep [-in kb/pdf] [-rawdir kb/raw] [-cleandir kb/clean] [-min-page-freq 0.5]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Guilherme-AVVO/mini-rag/internal/pdftext"
	"github.com/Guilherme-AVVO/mini-rag/internal/textclean"
)

// translitera mapeia as runas acentuadas comuns do português para ASCII.
// Mapa simples em vez de golang.org/x/text: o universo de entrada são nomes
// de arquivos de fichas técnicas em pt-BR, e não vale puxar uma dependência
// externa para meia dúzia de vogais acentuadas e o ç.
var translitera = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// reCopia reconhece o sufixo de cópia que navegadores/gestores de download
// acrescentam ao baixar o mesmo arquivo de novo: " (1)", " (2)"...
// Removê-lo evita slugs duplicados-com-número para o MESMO documento.
var reCopia = regexp.MustCompile(`\s*\(\d+\)\s*$`)

// reNaoAlnum casa qualquer sequência de caracteres não-alfanuméricos, que no
// slug vira UM único hífen (espaços, " - ", parênteses, etc.).
var reNaoAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converte o nome de um PDF em um slug estável para nomear os .txt:
// minúsculas, sem acento, sem sufixo de cópia, hífens no lugar de separadores.
// Ex.: "Ficha Técnica - Luminária Elevada LED media intensidade (1).pdf"
//
//	→ "ficha-tecnica-luminaria-elevada-led-media-intensidade"
//
// O slug é o que vira a coluna `documento` no banco (via ingest), então ele
// precisa ser determinístico: o mesmo PDF re-baixado gera o mesmo slug.
func slugify(nome string) string {
	nome = strings.TrimSuffix(nome, filepath.Ext(nome))
	// O sufixo de cópia sai ANTES da troca por hífens; depois dela, "(1)"
	// já teria virado "-1" e não daria mais para distinguir de conteúdo real.
	nome = reCopia.ReplaceAllString(nome, "")
	nome = strings.ToLower(nome)
	nome = strings.Map(func(r rune) rune {
		if t, ok := translitera[r]; ok {
			return t
		}
		return r
	}, nome)
	nome = reNaoAlnum.ReplaceAllString(nome, "-")
	return strings.Trim(nome, "-")
}

func main() {
	in := flag.String("in", "kb/pdf", "diretório com os PDFs de entrada")
	rawDir := flag.String("rawdir", "kb/raw", "diretório de saída do texto cru")
	cleanDir := flag.String("cleandir", "kb/clean", "diretório de saída do texto limpo")
	minPageFreq := flag.Float64("min-page-freq", 0.5,
		"fração mínima de páginas para uma linha repetida ser tratada como rodapé")
	flag.Parse()

	pdfs, err := filepath.Glob(filepath.Join(*in, "*.pdf"))
	if err != nil {
		log.Fatalf("listar PDFs em %s: %v", *in, err)
	}
	if len(pdfs) == 0 {
		log.Fatalf("nenhum *.pdf encontrado em %s (use -in para apontar o diretório certo)", *in)
	}

	for _, d := range []string{*rawDir, *cleanDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatalf("criar diretório %s: %v", d, err)
		}
	}

	inicio := time.Now()
	for _, pdf := range pdfs {
		slug := slugify(filepath.Base(pdf))
		if slug == "" {
			// Nome só de símbolos/acentos não mapeados geraria arquivo ".txt"
			// sem nome — melhor parar e pedir um nome de arquivo decente.
			log.Fatalf("slug vazio para %q: renomeie o arquivo", pdf)
		}

		paginas, err := pdftext.Extract(pdf)
		if err != nil {
			// Tudo-ou-nada: parar no primeiro erro evita um kb/ pela metade,
			// que a ingestão consumiria em silêncio.
			log.Fatalf("extrair %s: %v", pdf, err)
		}

		// Raw: páginas cruas juntadas com "\n\f\n" — o MESMO separador que o
		// textclean usa na saída, para o diff raw×clean alinhar página a página.
		raw := strings.Join(paginas, "\n\f\n")
		rawPath := filepath.Join(*rawDir, slug+".txt")
		if err := os.WriteFile(rawPath, []byte(raw), 0o644); err != nil {
			log.Fatalf("gravar %s: %v", rawPath, err)
		}

		limpo := textclean.CleanComOpcoes(paginas, *minPageFreq)
		cleanPath := filepath.Join(*cleanDir, slug+".txt")
		if err := os.WriteFile(cleanPath, []byte(limpo), 0o644); err != nil {
			log.Fatalf("gravar %s: %v", cleanPath, err)
		}

		// Contagem de rodapés só para o log. RodapesRepetidos normaliza cada
		// linha internamente (normalizaLinha cuida de nbsp/espaços/\r), então
		// chamar com as páginas cruas dá o mesmo resultado que dentro do Clean
		// — a normalização extra que o Clean faz antes é por linha também.
		rodapes := textclean.RodapesRepetidos(paginas, *minPageFreq)
		log.Printf("  %-55s %3d páginas, %d linhas-rodapé detectadas", slug, len(paginas), len(rodapes))
	}

	fmt.Printf("pdfprep: %d PDFs processados em %s (raw=%s clean=%s)\n",
		len(pdfs), time.Since(inicio).Round(time.Millisecond), *rawDir, *cleanDir)
}
