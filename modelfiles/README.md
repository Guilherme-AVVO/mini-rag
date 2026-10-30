# Modelfiles dos geradores `-teste` do Mini RAG

Cada `*-teste` é o **modelo base + os parâmetros de geração FIXOS** das Regras do projeto
(temp 0.1 · top_p 0.9 · top_k 20 · repeat_penalty 1.05 · num_ctx 16384). Mudar qualquer
parâmetro **invalida a comparação** entre modelos. O **system prompt NÃO fica aqui** — ele é
montado em `internal/rag/rag.go`.

> Antes não existiam salvos em lugar nenhum; criados aqui para reprodutibilidade.

## Tiers (arquitetura multi-modelo: leve p/ fácil, pesado p/ complexo)

| Modelo `-teste` | Base | Arquitetura | Disco | Papel |
|---|---|---|---|---|
| `gemma4-12b-teste` | `gemma4:12b` | denso | 7.6 GB | **padrão / tier leve** (100% na VRAM) |
| `gemma4-26b-teste` | `gemma4:26b` | **MoE** | 18 GB | tier médio (rápido sob offload) |
| `gemma4-31b-teste` | `gemma4:31b` | denso | 20 GB | tier pesado (mais lento — denso) |
| `qwen3.6-27b-teste` | `qwen3.6:27b` | denso | 17 GB | tier pesado (offload leve) |
| `qwen3.6-35b-teste` | `qwen3.6:35b` | **MoE A3B** | 24 GB | tier pesado (pesado mas rápido) |

## Como (re)criar

```bash
# 1) baixar a base (pin no tag explícito — evitar :latest p/ reprodutibilidade)
ollama pull gemma4:31b
ollama pull qwen3.6:27b
ollama pull qwen3.6:35b

# 2) criar o -teste a partir do Modelfile
ollama create gemma4-31b-teste  -f gemma4-31b-teste.Modelfile
ollama create qwen3.6-27b-teste -f qwen3.6-27b-teste.Modelfile
ollama create qwen3.6-35b-teste -f qwen3.6-35b-teste.Modelfile

# 3) conferir
ollama list | grep teste
```

Hardware-alvo: RTX 3060 12 GB VRAM + 32 GB RAM. Todos cabem @ ctx 16384 (os densos/MoE grandes
usam offload p/ RAM). A comparação entre modelos foi feita com o mesmo harness e a mesma base (ver `docs/RESULTADOS.md`).
