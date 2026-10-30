# Resultados da pesquisa (base privada original)

Medições feitas em junho de 2026 sobre a base privada: 15 documentos técnicos (14 fichas e 1 manual), 36 chunks, embeddings `bge-m3`, `top_k=5`, GPU RTX 3060 12 GB. Os documentos e as perguntas não foram publicados; aqui ficam só os números agregados e as conclusões.

## 1. Contextual retrieval — A/B

**Método:** as mesmas 45 perguntas (6 categorias), o mesmo corpus, LLM, temperatura e `top_k`. A única variável foi `RETRIEVAL_MODE`. O servidor registra o id de cada chunk recuperado, o que permitiu medir a recuperação a nível de chunk contra o `chunk_gabarito`. As 4 perguntas fora da base não entram no Recall/MRR (n = 41).

| | Recall@5 | MRR |
|---|---|---|
| Baseline (semantic) | 39/41 = 95% | 0,882 |
| Contextual | 41/41 = **100%** | **0,976** |

MRR por categoria:

| Categoria | Baseline | Contextual |
|---|---|---|
| fato_exato | 0,882 | 0,971 |
| desambiguacao | 0,854 | 0,938 |
| tabela | 0,762 | 1,000 |
| conflito_fonte | 1,000 | 1,000 |
| raciocinio | 1,000 | 1,000 |

- As 2 perguntas que o baseline não recuperava no top-5 passaram para as posições 1 e 2. Outras 4 subiram para a 1ª posição. Nenhuma piorou.
- Um documento com vocabulário parecido com os demais (potência, corrente, intensidade) aparecia no topo de buscas que não eram sobre ele; com o contexto, isso parou.
- Resposta julgada: 40 → 41 acertos de 45. Os erros que sobraram são de **geração** (ler a coluna errada de uma tabela achatada, não sinalizar divergência entre fontes, síntese incompleta), que o retrieval não resolve.

**Decisão:** `RETRIEVAL_MODE=contextual` como padrão; a próxima frente é a camada de geração.

**Ressalvas:** n = 45 e corpus pequeno (36 chunks). O ganho absoluto de MRR é modesto, mas recuperou 2 falhas reais de recall sem regressão.

## 2. Escolha do modelo gerador — bateria V2

18 perguntas difíceis (multi-hop, cálculo, conflito de fontes, tabela, síntese, armadilha), modo contextual, parâmetros fixos (`modelfiles/`). "off" = pipeline de 1 passo; "on" = modo 2 agentes.

| Modelo | off | on | VRAM | tok/s | 100% GPU? |
|---|---|---|---|---|---|
| **gemma4-12b** | **16/18** | 12/18 | ~9 GB | ~34 | sim |
| qwen3-8b | 14/18 | 11/18 | ~8,4 GB | ~51 | sim |
| granite4.1-8b | 13/18 | 14/18 | ~8,9 GB | ~47 | sim |
| aya-expanse-8b | 7/18 | 5/18 | ~7,5 GB | ~49 | sim |
| qwen3.6-35b (referência) | 16/18 | — | offload | ~30 | não |
| gemma4-26b (referência) | 15/18 | — | offload | ~30 | não |

- **gemma4-12b** empatou com o modelo de 35B sem offload e acertou justamente as perguntas que derrubaram os outros: sinalizou divergência entre fontes, resistiu à pergunta-armadilha e citou dois códigos equivalentes. Errou uma tabela achatada (ninguém acertou — problema de ingestão) e um cálculo que confundia corrente com potência.
- **Modo 2 agentes:** piorou gemma4-12b (−4), qwen3-8b (−3) e aya (−2); só o granite melhorou (+1). Contexto maior e mais ruidoso confunde o gerador. Fica desligado por padrão.
- **Tier pesado** (26B/35B) descartado: não cabe na VRAM e não supera o gemma4-12b.

## 3. VRAM (RTX 3060 12 GB, ctx 16384, gerador + bge-m3 residentes)

| Modelo | Gerador | bge-m3 | Total em uso | % de 12 GB |
|---|---|---|---|---|
| aya-expanse-8b | 6,4 GB | 0,7 GB | ~7,5 GB | 61% |
| qwen3-8b | 7,5 GB | 0,7 GB | ~8,4 GB | 70% |
| granite4.1-8b | 8,0 GB | 0,7 GB | ~8,9 GB | 74% |

O consumo vem do modelo quantizado (~5 GB), do KV cache do contexto de 16k e do `bge-m3` compartilhado.
