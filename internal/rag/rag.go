// internal/rag/rag.go -> orquestrador RAG
// Fluxo: Pergunta -> Embed -> Busca vetorial -> Contexto
// -> pede a resposta ao LLM.
// Basicamente aqui vão ficar as regras de negócio do RAG
package rag

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/Guilherme-AVVO/mini-rag/internal/embed"
	"github.com/Guilherme-AVVO/mini-rag/internal/llm"
	"github.com/Guilherme-AVVO/mini-rag/internal/repository"
)

// Fonte é um chunk que embasou a resposta (transparência / citação)
type Fonte struct {
	ChunkID      int64 // id do chunk recuperado (p/ medir recall@k a nível de chunk)
	Documento    string
	Secao        string
	Similaridade float64
}

// Resposta é o resultado de uma pergunta ao RAG (vai virar o JSON da rota)
type Resposta struct {
	Texto    string
	Fontes   []Fonte
	Metricas llm.Metricas
}

// Estrutura que amarra os trẽs gataways/repos. Recebe tudo pronto (injeção)
type Service struct {
	emb  *embed.Embedder
	repo *repository.Repository
	llm  *llm.Gerador
	topk int    // quantos chunks entram no contexto final (vem do TOP_K)
	mode string // "semantic" (baseline) | "contextual" (embedding_ctx)

	// modo 2-agentes (Pesquisador -> multi-query -> Redator)
	agentMode   bool // AGENT_MODE=on liga o Pesquisador; off = pipeline 1-passo atual
	multiQueryN int  // máx. de sub-buscas geradas pelo Pesquisador
	agentTopK   int  // chunks recuperados POR sub-busca antes do merge
}

func New(emb *embed.Embedder, repo *repository.Repository, gen *llm.Gerador, topk int, mode string) *Service {
	if mode == "" {
		mode = "semantic"
	}
	return &Service{emb: emb, repo: repo, llm: gen, topk: topk, mode: mode, multiQueryN: 3, agentTopK: topk}
}

// EnableAgents liga o modo 2-agentes. multiQueryN/agentTopK <= 0 mantêm os defaults.
func (s *Service) EnableAgents(multiQueryN, agentTopK int) {
	s.agentMode = true
	if multiQueryN > 0 {
		s.multiQueryN = multiQueryN
	}
	if agentTopK > 0 {
		s.agentTopK = agentTopK
	}
}

// SystemPrompt é a instrução que força o comportamento open-book.
// É o que faz o modelo ANCORAR na fonte em vez de confabular.
// Sem indentação dentro da raw string: cada espaço aqui vai literal pro modelo.
const SystemPrompt = `Você é o assistente técnico de uma empresa de engenharia aeroportuária. Você ajuda os funcionários no dia a dia — de consulta a normas a cálculos, código,
processos e redação. Responda como um engenheiro experiente: claro, direto e natural.

TOM (adapte ao contexto)
- No dia a dia (dúvidas, conversa, pedidos rápidos): tom natural e direto.
- Saudações, agradecimentos e encerramentos NÃO são consultas — responda curto e cordial, sem buscar norma nem dizer "não consta".
- Ao redigir documentos (ofícios, notificações, laudos, comunicados): tom formal e estruturado.
- Se o pedido for vago quanto ao formato ou destino (ex.: "preciso de um texto sobre X"), pergunte rapidamente o objetivo, ou entregue uma versão e
ofereça ajustar.

FATOS NORMATIVOS E TÉCNICOS (normas, valores, distâncias, cores, requisitos, códigos)
- Baseie-se SOMENTE no CONTEXTO e cite a fonte no formato [documento §seção].
- Antes de dizer que algo não consta, verifique se o contexto cobre o caso por um termo mais geral ou equivalente — ex.: "aproximação de precisão" é uma
operação "por instrumento"; a regra da luz de borda no fim da pista está sob a luz de borda. Se uma regra mais geral cobre a pergunta, aplique-a e diga de
qual caso ela vem.
- Só responda "não consta no material fornecido" depois de realmente esgotar o contexto. Nunca invente norma, valor ou código, e corrija premissas erradas
do usuário com base na fonte (não concorde por gentileza).
- Ao tratar itens parecidos (ex.: tipos diferentes de luz), confirme na seção citada a qual item exato o trecho se refere — não atribua a característica
de um a outro.

CONHECIMENTO GERAL (raciocínio, cálculos, código, processos, escrita e assuntos fora das normas)
- Fazem parte do seu trabalho — responda com utilidade. NÃO comece a resposta com avisos genéricos ("conhecimento geral, não consta..."); sinalize a
origem só quando ajudar a entender, de forma natural (ex.: "isto é prática de engenharia, não está na norma").
- Em cálculos, explicite as premissas (ex.: contar ou não as extremidades) e use a mesma premissa em toda a resposta.
- NUNCA cite um número de norma (NBR, RBAC, ICAO, FAA, artigo, seção) que não esteja no CONTEXTO — nem como conhecimento geral, nem para "ilustrar". Se
pedirem normas que não estão no material, diga que não pode confirmar o número e oriente onde buscar (ex.: catálogo da ABNT, site da ANAC), SEM inventar
nem chutar a numeração. Número de norma só sai daqui se estiver citado na fonte.

Quando misturar fato normativo e raciocínio próprio, deixe claro o que vem da fonte (citado) e o que é seu raciocínio.`

// PesquisadorPrompt é o system prompt do Agente 1 (Pesquisador): decompõe a
// pergunta em sub-buscas objetivas para o retrieval. Saída: UMA busca por linha,
// sem numeração — o parse é por linha.
const PesquisadorPrompt = `Você decompõe uma pergunta técnica em buscas para um sistema de RAG sobre fichas técnicas e manuais de balizamento aeroportuário (luminárias, bases metálicas, transformadores de isolamento, baliza, biruta, PAPI, farol rotativo).

Gere de 1 a 3 buscas curtas e objetivas, UMA POR LINHA, sem numeração, sem aspas e sem texto extra.
- Se a pergunta combina fatos de documentos/tabelas diferentes (multi-hop), crie uma busca para cada fato.
- Use os termos técnicos mais prováveis de aparecer no documento (modelo, código FAA, grandeza).
- Se a pergunta é simples, devolva apenas uma busca equivalente a ela.`

// planejarBusca chama o Pesquisador e devolve as sub-buscas + a pergunta
// original (sempre incluída: o multi-query nunca recupera PIOR que o baseline).
func (s *Service) planejarBusca(ctx context.Context, pergunta string) []string {
	queries := []string{pergunta}
	resp, err := s.llm.Generate(ctx, PesquisadorPrompt, pergunta)
	if err != nil {
		return queries // falha do Pesquisador não derruba a pergunta: segue baseline
	}
	vistas := map[string]bool{normaliza(pergunta): true}
	for _, ln := range strings.Split(resp.Texto, "\n") {
		q := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(ln), "-*0123456789. "))
		if q == "" || vistas[normaliza(q)] {
			continue
		}
		vistas[normaliza(q)] = true
		queries = append(queries, q)
		if len(queries) >= s.multiQueryN+1 { // +1 = a pergunta original
			break
		}
	}
	// auditável no terminal: o que o Pesquisador decidiu buscar
	log.Printf("pesquisador: %d busca(s) -> %q", len(queries), queries)
	return queries
}

func normaliza(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// buscarMulti roda o retrieval para cada query e funde os resultados:
// dedupe por chunk_id (mantém a MAIOR similaridade), ordena e corta no topk final.
func (s *Service) buscarMulti(ctx context.Context, queries []string) ([]repository.Resultado, error) {
	melhor := make(map[int64]repository.Resultado)
	for _, q := range queries {
		vetor, err := s.emb.Embed(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("embedding %q: %w", q, err)
		}
		var hits []repository.Resultado
		if s.mode == "contextual" {
			hits, err = s.repo.SearchCtx(ctx, vetor, s.agentTopK)
		} else {
			hits, err = s.repo.Search(ctx, vetor, s.agentTopK)
		}
		if err != nil {
			return nil, fmt.Errorf("busca %q: %w", q, err)
		}
		for _, h := range hits {
			if atual, ok := melhor[h.ID]; !ok || h.Similaridade > atual.Similaridade {
				melhor[h.ID] = h
			}
		}
	}
	fundidos := make([]repository.Resultado, 0, len(melhor))
	for _, h := range melhor {
		fundidos = append(fundidos, h)
	}
	sort.Slice(fundidos, func(i, j int) bool { return fundidos[i].Similaridade > fundidos[j].Similaridade })
	// corte: com 1 query (modo off) isso reproduz o comportamento atual; com
	// multi-query, mantém o contexto no mesmo tamanho do baseline (s.topk).
	max := s.topk
	if s.agentMode && len(queries) > 1 {
		// contexto um pouco maior no modo agente: até topk+2, p/ não descartar
		// o chunk certo de uma sub-busca secundária (corpus pequeno: seguro).
		max = s.topk + 2
	}
	if len(fundidos) > max {
		fundidos = fundidos[:max]
	}
	return fundidos, nil
}

func (s *Service) Ask(ctx context.Context, pergunta string) (Resposta, error) {
	// Agente 1 (Pesquisador): decompõe em sub-buscas. Com AGENT_MODE=off é
	// apenas a própria pergunta — caminho idêntico ao pipeline original.
	queries := []string{pergunta}
	if s.agentMode {
		queries = s.planejarBusca(ctx, pergunta)
	}

	hits, err := s.buscarMulti(ctx, queries)
	if err != nil {
		return Resposta{}, err
	}
	// monta o contexto (rotulado com a fonte) e a lista de fontes
	var ctxText strings.Builder
	fontes := make([]Fonte, 0, len(hits))
	for _, h := range hits {
		// rótulo com a seção (quando existe) → o modelo vê DE QUAL item o
		// trecho fala e consegue citar fino: [manual-farol-rotativo §2.3 Fixação]
		if h.Secao != "" {
			fmt.Fprintf(&ctxText, "[%s §%s] %s\n\n", h.Documento, h.Secao, h.Conteudo)
		} else {
			fmt.Fprintf(&ctxText, "[%s] %s\n\n", h.Documento, h.Conteudo)
		}
		fontes = append(fontes, Fonte{
			ChunkID:      h.ID,
			Documento:    h.Documento,
			Secao:        h.Secao,
			Similaridade: h.Similaridade,
		})
	}
	// monta o prompt final e gera
	prompt := fmt.Sprintf("CONTEXTO:\n%s\nPERGUNTA:\n%s", ctxText.String(), pergunta)
	resp, err := s.llm.Generate(ctx, SystemPrompt, prompt)
	if err != nil {
		return Resposta{}, fmt.Errorf("geração:%w", err)
	}

	// Empacota a resposta
	return Resposta{
		Texto:    resp.Texto,
		Fontes:   fontes,
		Metricas: resp.Metricas,
	}, nil
}
