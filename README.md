# mini-rag — RAG local em Go (Ollama + pgvector)

Pesquisa de um assistente técnico corporativo que roda **100% local**: responde perguntas sobre fichas técnicas e manuais **citando a fonte** (`[documento §seção]`) e diz "não consta no material fornecido" em vez de inventar.

Este repositório é a **versão pública** de uma pesquisa feita para uma empresa de engenharia. O código é o mesmo; os documentos, o gabarito e os contextos foram **substituídos por um corpus fictício** (`kb/clean/`) para que nada da empresa fosse publicado. Os números em [Resultados](#resultados-da-pesquisa) foram medidos na base privada original.

## Como funciona

```
PDFs ──pdfprep──▶ texto limpo ──ingest──▶ chunks + embeddings (pgvector)
                                                   │
pergunta ──embed──▶ busca vetorial (top-k) ──▶ contexto ──▶ LLM ──▶ resposta + fontes (+ métricas)
```

1. **Ingestão (offline):** `pdfprep` extrai e limpa os PDFs (remove cabeçalho/rodapé repetido, desfaz hifenização, normaliza espaços); `ingest` divide em chunks com detecção de seção e página, gera embeddings `bge-m3` (1024 dims) e grava no pgvector. Re-ingerir não duplica.
2. **Consulta (online):** a pergunta vira embedding, a busca por cosseno traz os trechos mais próximos, o contexto é montado com o rótulo de cada fonte e o LLM responde citando.
3. **Contextual retrieval (opcional):** cada chunk ganha 1–2 frases que o situam no documento; `contexto + conteúdo` é embedado numa tabela paralela (`chunks_contexto`), sem tocar no baseline. Liga com `RETRIEVAL_MODE=contextual`.
4. **Modo 2 agentes (experimental):** um "Pesquisador" decompõe a pergunta em até 3 buscas, os resultados são fundidos e um "Redator" responde. Fica atrás de `AGENT_MODE=on` (desligado por padrão — ver resultados).

## Estrutura

| Caminho | Função |
|---|---|
| `cmd/pdfprep` | PDF → texto (`pdftotext -layout`) + limpeza automática |
| `cmd/ingest` | Chunk + embedding + gravação no pgvector (idempotente) |
| `cmd/ctx` | Pipeline do contextual retrieval (`export` / `import` / `backfill` / `all`) |
| `cmd/eval` | Recall@k e MRR a nível de chunk, nos dois modos, contra o gabarito |
| `cmd/server` | API HTTP (chi) — `POST /ask` |
| `internal/pdftext`, `internal/textclean` | Extração e limpeza de PDF |
| `internal/chunk` | Chunking com detecção de seção e página |
| `internal/embed`, `internal/llm` | Clientes do Ollama (embeddings e geração) |
| `internal/repository`, `internal/database` | Acesso ao pgvector |
| `internal/rag` | Orquestração e system prompt anti-confabulação |
| `db/init` | Schema (extensão `vector`, `chunks`, `chunks_contexto`) |
| `kb/clean` | Corpus **fictício** de demonstração (10 documentos) |
| `ctx_work/contextos.json` | Contextos dos chunks do corpus fictício |
| `eval/perguntas_gabarito.json` | 23 perguntas com resposta esperada e chunk-fonte |
| `modelfiles/` | Parâmetros fixos dos modelos testados (reprodutibilidade) |

## Como rodar

Pré-requisitos: Go ≥ 1.26, Docker com `docker compose`, [Ollama](https://ollama.com) e, para PDFs novos, `poppler-utils` (`pdftotext`).

```bash
cp .env.example .env                 # ajuste senha, porta e modelos
docker compose up -d                 # Postgres 17 + pgvector

ollama pull bge-m3
ollama pull gemma4:12b && ollama create gemma4-12b-teste -f modelfiles/gemma4-12b-teste.Modelfile

go run ./cmd/ingest                  # ingere kb/clean/*.txt (banco novo → ids 1..11)
go run ./cmd/ctx all                 # importa ctx_work/contextos.json e re-embeda
go run ./cmd/eval                    # Recall@5 e MRR: semantic × contextual

RETRIEVAL_MODE=contextual go run ./cmd/server   # ouve em :8080
curl -s -X POST 'localhost:8080/ask?format=text&verbose=true' \
  -H 'Content-Type: application/json' \
  -d '{"pergunta":"Qual a altura do mastro da biruta para heliponto elevado?"}'
```

`POST /ask` · corpo `{"pergunta": "..."}` · `?verbose=true` inclui métricas de geração · `?format=text` devolve texto em vez de JSON. A resposta JSON traz `{resposta, fontes:[{chunk_id, documento, secao, similaridade}], metricas?}`.

Para usar seus próprios PDFs: coloque-os em `kb/pdf/`, rode `go run ./cmd/pdfprep` e depois o `ingest`. Para gerar contextos novos, `go run ./cmd/ctx export` grava os chunks em `ctx_work/`; escreva os contextos (com um LLM) em `ctx_work/contextos.json` e rode `go run ./cmd/ctx all`.

Variáveis do `.env`: `POSTGRES_*` · `LLM_URL` · `LLM_MODEL` · `EMBED_MODEL` · `CHUNK_SIZE` · `TOP_K` · `RETRIEVAL_MODE` (`semantic` | `contextual`) · `AGENT_MODE` (`off` | `on`) · `HTTP_PORT`.

## Resultados da pesquisa

Medidos na **base privada original** (15 documentos técnicos, 36 chunks), LLM local, `top_k=5`. Detalhes em [`docs/RESULTADOS.md`](docs/RESULTADOS.md).

**Contextual retrieval (A/B, 45 perguntas):**

| | Recall@5 | MRR |
|---|---|---|
| Baseline (semantic) | 95% (39/41) | 0,882 |
| Contextual | **100% (41/41)** | **0,976** |

Recuperou as duas falhas de recall do baseline e nenhuma pergunta piorou. Os erros que sobraram eram de **geração** (leitura de tabela, completude), não de busca.

**Escolha do modelo (18 perguntas difíceis: multi-hop, cálculo, conflito de fontes, tabela, armadilha):**

| Modelo | Acertos | VRAM | Cabe 100% na GPU (12 GB)? |
|---|---|---|---|
| **gemma4-12b** | **16/18** | ~9 GB | sim |
| qwen3-8b | 14/18 | ~8,4 GB | sim |
| granite4.1-8b | 13/18 | ~8,9 GB | sim |
| aya-expanse-8b | 7/18 | ~7,5 GB | sim |
| qwen3.6-35b | 16/18 | offload | não |
| gemma4-26b | 15/18 | offload | não |

`gemma4-12b` empatou com o modelo de 35B sem sair da GPU e foi o que menos confabulou. O modo 2 agentes piorou 3 dos 4 modelos (gemma4-12b: 16 → 12) e ficou desligado por padrão.

## Corpus fictício

Os documentos de `kb/clean/` descrevem produtos inventados (códigos `AX-*`) com valores inventados. **Não use esses dados em projeto real.** Com 10 documentos curtos (11 chunks) a busca fica fácil: o `cmd/eval` dá Recall@5 de 100% nos dois modos (MRR 0,950 semantic e 0,933 contextual, medido com `bge-m3`). O corpus serve para rodar o pipeline de ponta a ponta, não para reproduzir o A/B — o ganho do contextual retrieval aparece quando há muitos chunks parecidos competindo.
