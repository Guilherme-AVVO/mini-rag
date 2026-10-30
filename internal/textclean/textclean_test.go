package textclean

import (
	"strings"
	"testing"
)

// TestRodapeRepetidoRemovido garante que linhas presentes em todas as páginas
// (rodapé/cabeçalho típico) somem, enquanto o conteúdo único de cada página
// é preservado.
func TestRodapeRepetidoRemovido(t *testing.T) {
	paginas := []string{
		"Conteúdo único da página um.\nACME SINALIZACAO LTDA.\nwww.exemplo.com.br",
		"Conteúdo único da página dois.\nACME SINALIZACAO LTDA.\nwww.exemplo.com.br",
		"Conteúdo único da página três.\nACME SINALIZACAO LTDA.\nwww.exemplo.com.br",
	}

	out := Clean(paginas)

	if strings.Contains(out, "ACME SINALIZACAO LTDA.") {
		t.Errorf("rodapé repetido deveria ter sido removido; saída:\n%s", out)
	}
	if strings.Contains(out, "www.exemplo.com.br") {
		t.Errorf("rodapé repetido (url) deveria ter sido removido; saída:\n%s", out)
	}
	for _, unico := range []string{"página um", "página dois", "página três"} {
		if !strings.Contains(out, unico) {
			t.Errorf("conteúdo único %q deveria ter sido preservado; saída:\n%s", unico, out)
		}
	}
}

// TestDuasPaginasNadaRemovidoPorRepeticao: com menos de 3 páginas a detecção
// de rodapé fica DESLIGADA — não há amostra estatística suficiente.
func TestDuasPaginasNadaRemovidoPorRepeticao(t *testing.T) {
	paginas := []string{
		"Conteúdo um.\nACME SINALIZACAO LTDA.",
		"Conteúdo dois.\nACME SINALIZACAO LTDA.",
	}

	if rep := RodapesRepetidos(paginas, 0.5); len(rep) != 0 {
		t.Errorf("com 2 páginas RodapesRepetidos deveria ser vazio, veio: %v", rep)
	}

	out := Clean(paginas)
	if got := strings.Count(out, "ACME SINALIZACAO LTDA."); got != 2 {
		t.Errorf("com 2 páginas a linha repetida deveria ser mantida nas duas (got %d); saída:\n%s", got, out)
	}
}

// TestNbspNormalizadoNaDeteccao: a mesma linha com nbsp numa página e espaço
// comum noutra precisa contar como UMA linha na detecção de repetição.
func TestNbspNormalizadoNaDeteccao(t *testing.T) {
	paginas := []string{
		"Texto da página um.\nData\u00a0da\u00a0emissão",
		"Texto da página dois.\nData da emissão",
		"Texto da página três.\nData\u00a0da emissão",
	}

	rep := RodapesRepetidos(paginas, 0.5)
	if !rep["Data da emissão"] {
		t.Errorf("linha com nbsp deveria casar com a versão de espaço comum; mapa: %v", rep)
	}

	out := Clean(paginas)
	if strings.Contains(out, "emissão") {
		t.Errorf("linha repetida (nas variantes nbsp/espaço) deveria sumir de todas as páginas; saída:\n%s", out)
	}
	if strings.Contains(out, "\u00a0") {
		t.Errorf("não deveria sobrar nbsp na saída:\n%q", out)
	}
}

// TestNumeroDePaginaRemovido: linhas que são só numeração ("3", "2/239")
// somem mesmo sem repetição (vale até para documento de 1 página).
func TestNumeroDePaginaRemovido(t *testing.T) {
	paginas := []string{
		"Primeira linha de conteúdo.\n  2/239  \nSegunda linha de conteúdo.\n3",
	}

	out := Clean(paginas)

	if strings.Contains(out, "2/239") {
		t.Errorf("linha \"2/239\" deveria ter sido removida; saída:\n%s", out)
	}
	for _, linha := range strings.Split(out, "\n") {
		if strings.TrimSpace(linha) == "3" {
			t.Errorf("linha \"3\" sozinha deveria ter sido removida; saída:\n%s", out)
		}
	}
	if !strings.Contains(out, "Primeira linha de conteúdo.") || !strings.Contains(out, "Segunda linha de conteúdo.") {
		t.Errorf("conteúdo real deveria ter sido preservado; saída:\n%s", out)
	}
}

// TestDeHifenizacao: junta apenas quando a continuação começa com minúscula.
func TestDeHifenizacao(t *testing.T) {
	paginas := []string{
		"A segurança opera-\ncional deve ser mantida.\nO guarda-\nCorpo permanece instalado.",
	}

	out := Clean(paginas)

	if !strings.Contains(out, "segurança operacional deve") {
		t.Errorf("\"opera-\" + \"cional\" deveria ter sido juntado; saída:\n%s", out)
	}
	if !strings.Contains(out, "guarda-\nCorpo") {
		t.Errorf("\"guarda-\" + \"Corpo\" (maiúscula) NÃO deveria ter sido juntado; saída:\n%s", out)
	}
}

// TestFormFeedEParagrafos: a saída precisa ter exatamente len(paginas)-1
// marcadores \f (cada um em linha própria) e manter parágrafos separados por
// \n\n — inclusive colapsando excesso de linhas em branco para isso.
func TestFormFeedEParagrafos(t *testing.T) {
	paginas := []string{
		"Primeiro parágrafo.\n\n\n\n\nSegundo parágrafo.",
		"Conteúdo da página dois.",
		"Conteúdo da página três.",
	}

	out := Clean(paginas)

	if got := strings.Count(out, "\f"); got != len(paginas)-1 {
		t.Errorf("esperava %d ocorrências de \\f, veio %d; saída:\n%q", len(paginas)-1, got, out)
	}
	if !strings.Contains(out, "\n\f\n") {
		t.Errorf("o \\f deveria estar em linha própria (\\n\\f\\n); saída:\n%q", out)
	}
	if !strings.Contains(out, "Primeiro parágrafo.\n\nSegundo parágrafo.") {
		t.Errorf("parágrafos deveriam ficar separados por \\n\\n (linha em branco única); saída:\n%q", out)
	}
}

// TestPaginaVaziaPreservada: uma página que fica vazia após a limpeza (ex.:
// só tinha número de página) continua contando um \f — senão o chunker
// atribuiria as páginas seguintes ao número errado.
func TestPaginaVaziaPreservada(t *testing.T) {
	paginas := []string{
		"Conteúdo da página um.",
		"  7  ", // vira vazia: é só numeração
		"Conteúdo da página três.",
	}

	out := Clean(paginas)

	if got := strings.Count(out, "\f"); got != 2 {
		t.Errorf("página vazia deve ser mantida na contagem: esperava 2 \\f, veio %d; saída:\n%q", got, out)
	}
}
