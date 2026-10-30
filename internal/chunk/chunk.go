// Package chunk quebra o texto cru das normas em pedaços menores ("chunks")
// prontos para virar embeddings.
//
// Por que isso importa: um embedding resume o "significado" de um texto num
// único vetor. Texto grande demais → o vetor vira uma "média" de vários
// assuntos e a busca perde precisão. Texto pequeno demais → perde contexto.
// O objetivo aqui é produzir pedaços de tamanho equilibrado (~maxTokens),
// evitando cortar uma ideia no meio sempre que possível.
//
// Além de cortar, este pacote agora identifica a SEÇÃO da norma a que cada
// chunk pertence (ex.: "154.5", "5.3.9 Runway edge lights"). Isso permite
// citação fina ("[rbac154 §5.3.9]") e — principalmente — dá ao modelo o
// rótulo de QUAL item cada trecho descreve, combatendo a conflação
// (atribuir a propriedade certa à luz errada).
//
// Cada chunk também carrega a PÁGINA do PDF em que começa (1-based): o texto
// de entrada marca a fronteira de páginas com um form-feed `\f` numa linha
// própria (páginas re-juntadas com "\n\f\n"), e o Split converte essa marca
// em metadado — o \f nunca vaza para o conteúdo do chunk.
package chunk

import (
	"regexp"
	"strings"
	"unicode"
)

// overlapTokens é quanto cada chunk repete do chunk anterior (em tokens
// aproximados). Esse "overlap" dá continuidade: se uma regra cai exatamente na
// fronteira entre dois chunks, ela ainda aparece inteira em pelo menos um deles.
// Mantemos BAIXO de propósito — overlap grande duplica muito conteúdo (mais
// embeddings e armazenamento) sem ganho real de qualidade.
const overlapTokens = 60

// Chunk é um pedaço de texto pronto para embedding, com a seção a que pertence.
type Chunk struct {
	Secao    string // ex.: "154.5 Disposições gerais", "5.3.9 Runway edge lights" ("" se desconhecida)
	Conteudo string
	Pagina   int // página do PDF onde o chunk começa, 1-based; 0 = desconhecida
}

// reSecao reconhece o INÍCIO de uma seção nos documentos da KB:
//
//	"154.401 Aplicabilidade"        (RBAC: número.número)
//	"5.3.9 Runway edge lights"      (ICAO/FAA: numeração hierárquica)
//	"APÊNDICE B ..." / "Appendix 4" (apêndices)
//	"EXEMPLO 3 — ..."               (arquivos de exemplos)
//	"CHAPTER 5" / "Attachment A"    (ICAO/FAA)
var reSecao = regexp.MustCompile(`^\s*((?:\d{1,3}\.)+\d{1,3}|AP[ÊE]NDICE\s+[A-Z]|Appendix\s+\w+|EXEMPLO\s+\d+|CHAPTER\s+\d+|Attachment\s+[A-Z])\.?\s*(\S.*)?$`)

// reCapRomano reconhece títulos de capítulo em algarismos romanos, comuns em
// manuais de fabricante: "IV – INSTALAÇÃO", "VII – PESQUISA DE DEFEITOS".
// Para não confundir com listas com hífen ("- item ..."), o lado esquerdo
// precisa ser SÓ algarismos romanos (1 a 8 chars de IVXLC) e o título precisa
// começar com letra maiúscula — a checagem de "maioria maiúscula" do título
// fica no secaoDe, porque regex não conta letras.
var reCapRomano = regexp.MustCompile(`^\s*([IVXLC]{1,8})\s*[–—-]\s+(\p{Lu}.*)$`)

// maxPalavrasTitulo limita o tamanho do título aceito no padrão numérico
// (N.N, N.N.N...). Cláusulas legais numeradas ("6.2 É expressamente proibida
// a reprodução total ou parcial deste...") casam o mesmo padrão de um título
// de seção, mas títulos reais são curtos. Acima de 8 palavras, tratamos como
// texto corrido, não como seção.
const maxPalavrasTitulo = 8

// cortaTitulo limita o título a ~60 caracteres para a citação ficar legível.
func cortaTitulo(titulo string) string {
	r := []rune(titulo)
	if len(r) > 60 {
		return string(r[:60])
	}
	return titulo
}

// maioriaMaiusculas diz se as letras de s são majoritariamente MAIÚSCULAS.
// É o que separa um título de capítulo ("INSTALAÇÃO EM TORRE METÁLICA") de
// uma frase comum que por acaso começa com maiúscula.
func maioriaMaiusculas(s string) bool {
	maiusc, minusc := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			maiusc++
		case unicode.IsLower(r):
			minusc++
		}
	}
	return maiusc > minusc
}

// secaoDe devolve o rótulo da seção se o parágrafo COMEÇA uma seção ("" se não).
// O rótulo junta o número/marcador com o início do título (até ~60 caracteres),
// para a citação ficar legível: "5.3.9 Runway edge lights", "IV – INSTALAÇÃO".
//
// Dois filtros evitam falsos positivos observados nos manuais:
//   - capítulo romano só vale se o título for majoritariamente MAIÚSCULAS
//     (senão "I - item de lista" viraria seção);
//   - padrão numérico só vale com título de até maxPalavrasTitulo palavras
//     (senão cláusulas legais numeradas viram seção). Linha só com o número
//     ("154.401") continua aceita.
func secaoDe(p string) string {
	primeiraLinha := p
	if i := strings.IndexByte(p, '\n'); i >= 0 {
		primeiraLinha = p[:i]
	}

	// capítulo em romano ("IV – INSTALAÇÃO"): título precisa "gritar" em caixa alta
	if m := reCapRomano.FindStringSubmatch(primeiraLinha); m != nil {
		titulo := strings.TrimSpace(m[2])
		if maioriaMaiusculas(titulo) {
			return m[1] + " – " + cortaTitulo(titulo)
		}
	}

	m := reSecao.FindStringSubmatch(primeiraLinha)
	if m == nil {
		return ""
	}
	rotulo := strings.TrimSpace(m[1])
	titulo := strings.TrimSpace(m[2])

	// padrão numérico com título longo é cláusula/frase corrida, não seção
	if rotulo[0] >= '0' && rotulo[0] <= '9' && len(strings.Fields(titulo)) > maxPalavrasTitulo {
		return ""
	}

	if titulo != "" {
		rotulo += " " + cortaTitulo(titulo)
	}
	return rotulo
}

// aproxTokens estima quantos tokens um texto tem a partir do número de palavras.
// Não usamos o tokenizer real do modelo porque, só para DIMENSIONAR o tamanho
// dos chunks, a regra de bolso "1 palavra ≈ 1,3 token" (média do português) já
// é precisa o suficiente e não custa nada.
func aproxTokens(s string) int {
	return int(float64(len(strings.Fields(s))) * 1.3)
}

// hardSplit é o "plano B" para um parágrafo que, sozinho, já passa de maxTokens.
// Nesse caso não dá para mantê-lo inteiro num chunk; quebramos em janelas de no
// máximo maxTokens, cortando por PALAVRA. É simples e robusto de propósito: não
// tentamos achar "fim de frase", porque texto normativo é cheio de "art.",
// "n.º", "inc." etc., e um split por pontuação erraria direto.
func hardSplit(p string, maxTokens int) []string {
	// converte o alvo de tokens em alvo de palavras (inverso do aproxTokens)
	maxPalavras := int(float64(maxTokens) / 1.3)
	if maxPalavras < 1 {
		maxPalavras = 1
	}

	palavras := strings.Fields(p)
	var out []string
	// avança de maxPalavras em maxPalavras, fatiando janelas consecutivas
	for i := 0; i < len(palavras); i += maxPalavras {
		fim := i + maxPalavras
		if fim > len(palavras) {
			fim = len(palavras)
		}
		out = append(out, strings.Join(palavras[i:fim], " "))
	}
	return out
}

// seg é um pedaço intermediário (<= maxTokens) já etiquetado com a seção
// corrente e a página em que o parágrafo está.
type seg struct {
	secao  string
	texto  string
	pagina int
}

// Split quebra o texto em chunks de ~maxTokens, em 3 fases:
//
//	Fase 0 — separa o texto em PÁGINAS pelo form-feed `\f` (o extrator de PDF
//	         re-junta as páginas com "\n\f\n", então o \f vive numa linha
//	         própria e funciona naturalmente como fronteira de parágrafo).
//	         Texto sem \f é tratado como uma página só → Pagina=1 para tudo.
//	Fase 1 — separa cada página em parágrafos (blocos divididos por linha em branco).
//	Fase 2 — transforma parágrafos em "segmentos" (todos <= maxTokens, via
//	         hardSplit quando preciso), rastreando a SEÇÃO corrente: quando um
//	         parágrafo começa com título de seção (secaoDe), a seção muda.
//	         A seção PERSISTE entre páginas — virar a página não fecha a seção.
//	Fase 3 — agrupa segmentos em chunks de ~maxTokens, repetindo só um pequeno
//	         "rabo" (overlapTokens) entre chunks vizinhos. Cada chunk herda a
//	         seção e a página do seu primeiro segmento.
//
// Devolve os chunks com Secao + Conteudo + Pagina; quem adiciona os demais
// metadados (documento, ordem, embedding) é o ingest.
func Split(texto string, maxTokens int) []Chunk {
	// ---------- Fase 0: páginas (fronteira = \f) ----------
	pags := strings.Split(texto, "\f")

	// ---------- Fases 1 e 2: parágrafos → segmentos <= maxTokens, etiquetados ----------
	var segs []seg
	secaoAtual := "" // persiste entre páginas: a seção corrente segue valendo ao virar a página
	for i, pag := range pags {
		pagina := i + 1 // páginas são 1-based, como o leitor de PDF enxerga

		// ---------- Fase 1: parágrafos ----------
		paras := strings.Split(pag, "\n\n")

		// ---------- Fase 2: segmentos <= maxTokens, etiquetados com seção e página ----------
		for _, p := range paras {
			p = strings.TrimSpace(p)
			if p == "" {
				continue // ignora blocos vazios
			}
			// este parágrafo abre uma nova seção? então ela vale daqui pra frente
			if s := secaoDe(p); s != "" {
				secaoAtual = s
			}
			if aproxTokens(p) <= maxTokens {
				segs = append(segs, seg{secaoAtual, p, pagina}) // cabe inteiro
			} else {
				for _, parte := range hardSplit(p, maxTokens) { // grande demais → quebra
					segs = append(segs, seg{secaoAtual, parte, pagina})
				}
			}
		}
	}

	// ---------- Fase 3: agrupa segmentos em chunks (com overlap enxuto) ----------
	var chunks []Chunk
	var buf []seg // segmentos do chunk que está sendo montado
	tok := 0      // tokens acumulados em buf

	junta := func(b []seg) string {
		partes := make([]string, len(b))
		for i, s := range b {
			partes[i] = s.texto
		}
		return strings.Join(partes, "\n\n")
	}

	// flush emite o chunk atual e reinicia buf mantendo só o "rabo" de overlap.
	flush := func() {
		if len(buf) == 0 {
			return
		}
		chunks = append(chunks, Chunk{Secao: buf[0].secao, Conteudo: junta(buf), Pagina: buf[0].pagina})

		// Overlap: guarda os ÚLTIMOS segmentos cuja soma cabe em overlapTokens.
		// Como respeitamos esse orçamento, segmentos grandes NUNCA são repetidos.
		var rabo []seg
		soma := 0
		for i := len(buf) - 1; i >= 0; i-- {
			t := aproxTokens(buf[i].texto)
			if soma+t > overlapTokens {
				break
			}
			rabo = append([]seg{buf[i]}, rabo...) // prepend p/ manter a ordem
			soma += t
		}
		buf = rabo
		tok = soma
	}

	for _, s := range segs {
		st := aproxTokens(s.texto)
		// se somar este segmento estoura o limite E já há conteúdo, fecha o chunk
		if tok+st > maxTokens && len(buf) > 0 {
			flush()
		}
		buf = append(buf, s)
		tok += st
	}

	// emite o que sobrou (último chunk)
	if len(buf) > 0 {
		chunks = append(chunks, Chunk{Secao: buf[0].secao, Conteudo: junta(buf), Pagina: buf[0].pagina})
	}

	return chunks
}
