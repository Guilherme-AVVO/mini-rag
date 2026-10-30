package chunk

import (
	"strings"
	"testing"
)

// paragrafo gera um parágrafo com n palavras (~1,3n tokens). Útil para montar
// textos com tamanho controlado sem poluir os testes com lorem ipsum.
func paragrafo(palavra string, n int) string {
	return strings.TrimSpace(strings.Repeat(palavra+" ", n))
}

// TestSplitPaginas verifica a rastreabilidade de página: texto com 3 páginas
// separadas por "\n\f\n" deve gerar chunks com Pagina 1-based correta e o \f
// nunca pode vazar para o Conteudo.
func TestSplitPaginas(t *testing.T) {
	// Cada página tem um parágrafo de 54 palavras (~70 tokens). Com
	// maxTokens=100, cada parágrafo vira um chunk próprio; e como 70 tokens
	// passam do orçamento de overlap (60), nenhum "rabo" atravessa de uma
	// página para a outra — o mapeamento chunk→página fica determinístico.
	p1 := paragrafo("um", 54)
	p2 := paragrafo("dois", 54)
	p3 := paragrafo("tres", 54)
	texto := p1 + "\n\f\n" + p2 + "\n\f\n" + p3

	chunks := Split(texto, 100)

	if len(chunks) != 3 {
		t.Fatalf("esperava 3 chunks (um por página), veio %d: %+v", len(chunks), chunks)
	}
	for i, c := range chunks {
		if c.Pagina != i+1 {
			t.Errorf("chunk %d: Pagina = %d, esperava %d", i, c.Pagina, i+1)
		}
		if strings.ContainsRune(c.Conteudo, '\f') {
			t.Errorf("chunk %d: \\f vazou para o Conteudo: %q", i, c.Conteudo)
		}
	}
	if !strings.HasPrefix(chunks[1].Conteudo, "dois") {
		t.Errorf("chunk 1 deveria conter o parágrafo da página 2, veio %q", chunks[1].Conteudo)
	}
}

// TestSplitSemFormFeed garante retrocompatibilidade: texto sem \f se comporta
// exatamente como antes (uma "página" só) e todos os chunks reportam Pagina=1.
func TestSplitSemFormFeed(t *testing.T) {
	texto := "154.401 Aplicabilidade\n\nEste requisito se aplica a aeródromos.\n\nSegundo parágrafo da mesma seção."

	chunks := Split(texto, 300)

	if len(chunks) != 1 {
		t.Fatalf("esperava 1 chunk, veio %d: %+v", len(chunks), chunks)
	}
	c := chunks[0]
	if c.Pagina != 1 {
		t.Errorf("Pagina = %d, esperava 1 (texto sem \\f é uma página só)", c.Pagina)
	}
	if c.Secao != "154.401 Aplicabilidade" {
		t.Errorf("Secao = %q, esperava %q", c.Secao, "154.401 Aplicabilidade")
	}
	quer := "154.401 Aplicabilidade\n\nEste requisito se aplica a aeródromos.\n\nSegundo parágrafo da mesma seção."
	if c.Conteudo != quer {
		t.Errorf("Conteudo mudou em relação ao comportamento atual:\n got: %q\nquer: %q", c.Conteudo, quer)
	}
}

// TestSecaoPersisteEntrePaginas: uma seção aberta na página 1 continua valendo
// para parágrafos da página 2 que não abrem seção nova — virar a página não
// fecha a seção.
func TestSecaoPersisteEntrePaginas(t *testing.T) {
	// Parágrafos de ~70 tokens (> overlap de 60) para o chunk da página 2
	// começar de fato na página 2, sem rabo herdado da página 1.
	texto := "4.1 INSTALAÇÃO\n\n" + paragrafo("alfa", 54) + "\n\f\n" + paragrafo("beta", 54)

	chunks := Split(texto, 100)

	if len(chunks) != 2 {
		t.Fatalf("esperava 2 chunks, veio %d: %+v", len(chunks), chunks)
	}
	if chunks[1].Pagina != 2 {
		t.Errorf("segundo chunk: Pagina = %d, esperava 2", chunks[1].Pagina)
	}
	if chunks[1].Secao != "4.1 INSTALAÇÃO" {
		t.Errorf("segundo chunk: Secao = %q, esperava herdar %q da página anterior", chunks[1].Secao, "4.1 INSTALAÇÃO")
	}
}

// TestSecaoDe cobre os padrões de detecção de seção, inclusive os filtros
// anti-falso-positivo (cláusula legal numerada, lista com hífen) e os novos
// capítulos em algarismos romanos.
func TestSecaoDe(t *testing.T) {
	casos := []struct {
		nome string
		in   string
		quer string
	}{
		{
			nome: "cláusula legal numerada com título longo NÃO é seção",
			in:   "6.2 É expressamente proibida a reprodução total ou parcial deste",
			quer: "",
		},
		{
			nome: "capítulo romano em caixa alta É seção",
			in:   "IV – INSTALAÇÃO",
			quer: "IV – INSTALAÇÃO",
		},
		{
			nome: "capítulo romano composto É seção",
			in:   "VII – PESQUISA DE DEFEITOS",
			quer: "VII – PESQUISA DE DEFEITOS",
		},
		{
			nome: "numeração hierárquica com título curto É seção",
			in:   "4.2.2 INSTALAÇÃO EM TORRE METÁLICA",
			quer: "4.2.2 INSTALAÇÃO EM TORRE METÁLICA",
		},
		{
			nome: "número sozinho (sem título) É seção",
			in:   "154.401",
			quer: "154.401",
		},
		{
			nome: "lista com hífen NÃO é seção",
			in:   "- item de uma lista qualquer",
			quer: "",
		},
		{
			nome: "título curto em caixa mista continua aceito",
			in:   "2.3 Recomendação de Peças de Reposição",
			quer: "2.3 Recomendação de Peças de Reposição",
		},
		{
			nome: "romano com frase em caixa baixa NÃO é seção",
			in:   "I - Item comum de lista que só começa com maiúscula",
			quer: "",
		},
		{
			nome: "apêndice continua aceito",
			in:   "APÊNDICE B Sinalização luminosa",
			quer: "APÊNDICE B Sinalização luminosa",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := secaoDe(c.in); got != c.quer {
				t.Errorf("secaoDe(%q) = %q, esperava %q", c.in, got, c.quer)
			}
		})
	}
}

// TestSplitClausulaLegalNaoViraSecao reproduz o caso real do manual do farol
// rotativo: o chunk com a cláusula de direitos autorais não pode receber o
// rótulo "6.2 É expressamente proibida...". Ele deve herdar a seção anterior.
func TestSplitClausulaLegalNaoViraSecao(t *testing.T) {
	texto := "6.1 GARANTIA\n\nO equipamento tem garantia de 12 meses.\n\n6.2 É expressamente proibida a reprodução total ou parcial deste manual sem autorização."

	chunks := Split(texto, 300)

	if len(chunks) != 1 {
		t.Fatalf("esperava 1 chunk, veio %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Secao != "6.1 GARANTIA" {
		t.Errorf("Secao = %q, esperava %q (cláusula legal não abre seção)", chunks[0].Secao, "6.1 GARANTIA")
	}
}
