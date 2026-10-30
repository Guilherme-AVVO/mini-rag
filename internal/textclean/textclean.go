// Package textclean limpa o texto cru extraído de PDFs (via pdftotext -layout)
// antes do chunking.
//
// Por que isso importa: o pdftotext devolve o texto "como está na página", o
// que inclui lixo que envenena os embeddings — rodapés e cabeçalhos repetidos
// em toda página ("EMPRESA X LTDA.", "www.exemplo.com.br", CNPJ),
// números de página soltos, espaços não-quebráveis (U+00A0) que impedem
// comparações de string, e palavras hifenizadas na quebra de linha ("opera-"
// + "cional"). Um chunk cheio de rodapé vira um vetor que "fala de rodapé",
// e a busca semântica perde precisão.
//
// A peça central aqui é a detecção AUTOMÁTICA de rodapé/cabeçalho: em vez de
// manter um regex hardcoded por documento (frágil — o padrão muda a cada PDF),
// observamos que rodapé é, por definição, a linha que se REPETE em quase toda
// página. Então contamos em quantas páginas distintas cada linha aparece e
// removemos as que passam de um limiar (default: metade das páginas).
package textclean

import (
	"regexp"
	"strings"
	"unicode"
)

// minFracDefault é a fração mínima de páginas em que uma linha precisa
// aparecer para ser considerada rodapé/cabeçalho. Metade é um equilíbrio
// seguro: rodapé de verdade aparece em ~100% das páginas; uma frase de
// conteúdo real dificilmente se repete idêntica em metade do documento.
const minFracDefault = 0.5

// minPaginasDeteccao é o mínimo de páginas para ativar a detecção de
// repetição. Com 1–2 páginas a estatística não diz nada: qualquer linha
// repetida por coincidência seria apagada, então preferimos não remover nada.
const minPaginasDeteccao = 3

// minRunesRodape é o tamanho mínimo (em runas) de uma linha candidata a
// rodapé. Linhas curtíssimas ("a)", "ou", "—") repetem-se naturalmente no
// conteúdo e NÃO podem ser removidas em massa.
const minRunesRodape = 4

// normEspacos converte os espaços unicode "exóticos" que o pdftotext emite
// em espaço comum. Sem isso, "Data da emissão" com nbsp e com espaço normal
// seriam strings DIFERENTES e a detecção de rodapé não casaria as duas.
var normEspacos = strings.NewReplacer(
	"\u00a0", " ", // no-break space (o mais comum em PDFs)
	"\u2007", " ", // figure space
	"\u202f", " ", // narrow no-break space
	"\u200b", " ", // zero-width space
)

// reEspacos colapsa sequências de espaços/tabs internos. O -layout do
// pdftotext alinha colunas com espaços variáveis, então a MESMA linha de
// rodapé pode vir com espaçamentos diferentes em páginas diferentes.
var reEspacos = regexp.MustCompile(`\s+`)

// reNumPagina reconhece linhas que são só numeração de página: "3" ou "2/239".
// Limitamos a 3 dígitos para não apagar, por engano, linhas com números
// longos que sejam conteúdo (códigos, anos com contexto ficam de fora porque
// teriam mais texto na linha).
var reNumPagina = regexp.MustCompile(`^\d{1,3}(\s*/\s*\d{1,3})?$`)

// normalizaLinha reduz uma linha à sua "forma canônica" para comparação:
// espaços unicode viram espaço comum, espaços nas pontas somem e espaços
// internos múltiplos colapsam em um. É essa forma que entra na contagem de
// repetição — assim variações cosméticas da mesma linha contam como iguais.
func normalizaLinha(ln string) string {
	ln = normEspacos.Replace(ln)
	ln = strings.TrimSpace(ln)
	return reEspacos.ReplaceAllString(ln, " ")
}

// RodapesRepetidos identifica linhas que se repetem em pelo menos minFrac das
// páginas (candidatas a cabeçalho/rodapé). Só ativa com 3+ páginas.
//
// A contagem é por PÁGINA DISTINTA, não por ocorrência: uma linha que aparece
// 10 vezes na mesma página (ex.: célula de tabela) conta 1 — repetição dentro
// da página não é evidência de rodapé. As chaves do mapa são as linhas já na
// forma canônica de normalizaLinha.
func RodapesRepetidos(paginas []string, minFrac float64) map[string]bool {
	rodapes := make(map[string]bool)
	if len(paginas) < minPaginasDeteccao {
		return rodapes // amostra pequena demais: não remove nada
	}

	// conta em quantas páginas distintas cada linha canônica aparece
	cont := make(map[string]int)
	for _, pg := range paginas {
		vistas := make(map[string]bool) // dedup dentro da página
		for _, ln := range strings.Split(pg, "\n") {
			n := normalizaLinha(ln)
			if len([]rune(n)) < minRunesRodape || vistas[n] {
				continue
			}
			vistas[n] = true
			cont[n]++
		}
	}

	limiar := minFrac * float64(len(paginas))
	for n, c := range cont {
		if float64(c) >= limiar {
			rodapes[n] = true
		}
	}
	return rodapes
}

// Clean limpa o texto extraído de um PDF (página a página) e devolve o texto
// pronto para o chunking, com \f marcando o início de cada nova página.
func Clean(paginas []string) string {
	return CleanComOpcoes(paginas, minFracDefault)
}

// CleanComOpcoes é o Clean com o limiar de detecção de rodapé configurável.
// minFrac é a fração de páginas em que uma linha precisa repetir para ser
// tratada como rodapé/cabeçalho (Clean usa 0.5).
func CleanComOpcoes(paginas []string, minFrac float64) string {
	// ---------- Etapa 1: normalização de caracteres ----------
	// Fazemos isso ANTES da detecção de rodapé para que a mesma linha com
	// nbsp numa página e espaço comum noutra conte como a mesma linha.
	norm := make([]string, len(paginas))
	for i, pg := range paginas {
		pg = strings.ReplaceAll(pg, "\r\n", "\n")
		pg = normEspacos.Replace(pg)
		linhas := strings.Split(pg, "\n")
		for j, ln := range linhas {
			linhas[j] = strings.TrimRight(ln, " \t")
		}
		norm[i] = strings.Join(linhas, "\n")
	}

	// ---------- Etapa 2: detecção automática de rodapé/cabeçalho ----------
	rodapes := RodapesRepetidos(norm, minFrac)

	// ---------- Etapas 3–5: limpeza página a página ----------
	// Páginas que ficam vazias após a limpeza são MANTIDAS no slice: o \f é o
	// marcador de página do chunker, e pular uma página desalinharia a
	// contagem (a citação apontaria para a página errada).
	limpas := make([]string, len(norm))
	for i, pg := range norm {
		limpas[i] = limpaPagina(pg, rodapes)
	}

	// ---------- Etapa 6: re-junção com \f em linha própria ----------
	return strings.Join(limpas, "\n\f\n")
}

// limpaPagina aplica a uma página as remoções (rodapé, número de página), a
// de-hifenização e a normalização de espaçamento vertical.
func limpaPagina(pg string, rodapes map[string]bool) string {
	linhas := strings.Split(pg, "\n")

	// Remoção de rodapés detectados e de linhas de número de página.
	mantidas := make([]string, 0, len(linhas))
	for _, ln := range linhas {
		t := strings.TrimSpace(ln)
		if rodapes[normalizaLinha(ln)] {
			continue
		}
		if t != "" && reNumPagina.MatchString(t) {
			continue
		}
		mantidas = append(mantidas, ln)
	}

	mantidas = deHifeniza(mantidas)

	// Espaçamento: colapsa 3+ linhas em branco consecutivas em UMA linha em
	// branco. Runs de 1–2 ficam como estão — uma linha em branco (\n\n) é a
	// separação de parágrafo que o chunker espera, e não queremos colar
	// parágrafos vizinhos. Linhas em branco no início/fim da página somem.
	var fin []string
	brancos := 0
	for _, ln := range mantidas {
		if strings.TrimSpace(ln) == "" {
			brancos++
			continue
		}
		if len(fin) > 0 { // ignora brancos antes da primeira linha de conteúdo
			if brancos >= 3 {
				fin = append(fin, "")
			} else {
				for k := 0; k < brancos; k++ {
					fin = append(fin, "")
				}
			}
		}
		brancos = 0
		fin = append(fin, ln)
	}

	return strings.TrimSpace(strings.Join(fin, "\n"))
}

// deHifeniza junta palavras quebradas por hífen no fim da linha, de forma
// CONSERVADORA: só junta quando há letra antes do hífen E a próxima linha
// não-vazia começa com letra MINÚSCULA ("exem-" + "plo" → "exemplo").
//
// Se a continuação começa com maiúscula ou dígito, NÃO junta: pode ser hífen
// legítimo (item de lista, nome composto como "guarda-Corpo", intervalo).
// Preferimos deixar um hífen sobrando a colar duas palavras que não eram uma.
func deHifeniza(linhas []string) []string {
	out := make([]string, 0, len(linhas))
	for i := 0; i < len(linhas); i++ {
		ln := linhas[i]
		// laço: a linha já juntada pode, ela mesma, terminar hifenizada
		for terminaEmHifenDePalavra(ln) {
			// procura a próxima linha não-vazia (a continuação da palavra)
			j := i + 1
			for j < len(linhas) && strings.TrimSpace(linhas[j]) == "" {
				j++
			}
			if j >= len(linhas) {
				break // hífen na última linha da página: deixa como está
			}
			prox := strings.TrimSpace(linhas[j])
			r := []rune(prox)
			if len(r) == 0 || !unicode.IsLower(r[0]) {
				break // continuação com maiúscula/dígito: hífen legítimo
			}
			ln = strings.TrimSuffix(ln, "-") + prox
			i = j // a linha consumida (e os brancos no meio) saem da saída
		}
		out = append(out, ln)
	}
	return out
}

// terminaEmHifenDePalavra diz se a linha termina em "-" colado a uma letra
// (indício de palavra quebrada). "10-" ou "— -" não contam: o que vem antes
// do hífen não é letra, então não é hifenização de palavra.
func terminaEmHifenDePalavra(ln string) bool {
	if !strings.HasSuffix(ln, "-") {
		return false
	}
	r := []rune(strings.TrimSuffix(ln, "-"))
	return len(r) > 0 && unicode.IsLetter(r[len(r)-1])
}
