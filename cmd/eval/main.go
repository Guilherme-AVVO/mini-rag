// cmd/eval: mede a RECUPERAÇÃO (sem gerar resposta) contra o gabarito.
//
// Para cada pergunta com chunk_gabarito, embeda a pergunta, busca o top-k nos
// dois modos (semantic = embedding do conteúdo; contextual = embedding de
// contexto + conteúdo) e calcula Recall@k e MRR a nível de chunk, no total e
// por categoria. Perguntas fora_kb (sem chunk de resposta) não entram.
//
// Uso: go run ./cmd/eval [-k 5] [-gabarito eval/perguntas_gabarito.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/Guilherme-AVVO/mini-rag/internal/config"
	"github.com/Guilherme-AVVO/mini-rag/internal/database"
	"github.com/Guilherme-AVVO/mini-rag/internal/embed"
	"github.com/Guilherme-AVVO/mini-rag/internal/repository"
)

type alvo struct {
	Documento string `json:"documento"`
	Ordem     int    `json:"ordem"`
}

type pergunta struct {
	ID            string `json:"id"`
	Categoria     string `json:"categoria"`
	Pergunta      string `json:"pergunta"`
	ChunkGabarito []alvo `json:"chunk_gabarito"`
}

type placar struct {
	n, acertos int
	somaRR     float64
}

func (p *placar) soma(rank int) {
	p.n++
	if rank > 0 {
		p.acertos++
		p.somaRR += 1 / float64(rank)
	}
}

func main() {
	k := flag.Int("k", 5, "tamanho do top-k")
	arq := flag.String("gabarito", "eval/perguntas_gabarito.json", "arquivo do gabarito")
	flag.Parse()

	b, err := os.ReadFile(*arq)
	if err != nil {
		log.Fatalf("ler gabarito: %v", err)
	}
	var gab struct {
		Perguntas []pergunta `json:"perguntas"`
	}
	if err := json.Unmarshal(b, &gab); err != nil {
		log.Fatalf("parse gabarito: %v", err)
	}

	ctx := context.Background()
	cfg := config.LoadConfig()
	pool, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("conectar ao banco: %v", err)
	}
	defer pool.Close()
	repo := repository.New(pool)
	emb, err := embed.New(cfg.LLM_URL, cfg.EMBED_MODEL)
	if err != nil {
		log.Fatalf("criar embedder: %v", err)
	}

	// resolve (documento, ordem) -> id do chunk no banco atual
	idDe := func(a alvo) int64 {
		var id int64
		err := pool.QueryRow(ctx, `SELECT id FROM chunks WHERE documento=$1 AND ordem=$2`, a.Documento, a.Ordem).Scan(&id)
		if err != nil {
			log.Fatalf("chunk %s#%d não está no banco (rode o ingest): %v", a.Documento, a.Ordem, err)
		}
		return id
	}

	modos := []string{"semantic", "contextual"}
	total := map[string]*placar{}
	porCat := map[string]map[string]*placar{}
	for _, m := range modos {
		total[m] = &placar{}
		porCat[m] = map[string]*placar{}
	}

	fmt.Printf("%-4s %-15s %8s %10s\n", "id", "categoria", "semantic", "contextual")
	for _, q := range gab.Perguntas {
		if len(q.ChunkGabarito) == 0 {
			continue
		}
		certos := map[int64]bool{}
		for _, a := range q.ChunkGabarito {
			certos[idDe(a)] = true
		}
		vetor, err := emb.Embed(ctx, q.Pergunta)
		if err != nil {
			log.Fatalf("embedding %s: %v", q.ID, err)
		}
		ranks := map[string]int{}
		for _, m := range modos {
			var hits []repository.Resultado
			if m == "contextual" {
				hits, err = repo.SearchCtx(ctx, vetor, *k)
			} else {
				hits, err = repo.Search(ctx, vetor, *k)
			}
			if err != nil {
				log.Fatalf("busca %s (%s): %v", q.ID, m, err)
			}
			// rank = posição (1-based) do PRIMEIRO chunk correto; 0 = fora do top-k
			rank := 0
			for i, h := range hits {
				if certos[h.ID] {
					rank = i + 1
					break
				}
			}
			ranks[m] = rank
			total[m].soma(rank)
			if porCat[m][q.Categoria] == nil {
				porCat[m][q.Categoria] = &placar{}
			}
			porCat[m][q.Categoria].soma(rank)
		}
		fmt.Printf("%-4s %-15s %8d %10d\n", q.ID, q.Categoria, ranks["semantic"], ranks["contextual"])
	}

	fmt.Printf("\n%-15s %18s %18s\n", "", "semantic", "contextual")
	linha := func(nome string, s, c *placar) {
		fmt.Printf("%-15s R@%d %3.0f%% MRR %.3f  R@%d %3.0f%% MRR %.3f\n", nome,
			*k, 100*float64(s.acertos)/float64(s.n), s.somaRR/float64(s.n),
			*k, 100*float64(c.acertos)/float64(c.n), c.somaRR/float64(c.n))
	}
	cats := make([]string, 0, len(porCat["semantic"]))
	for c := range porCat["semantic"] {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, c := range cats {
		linha(c, porCat["semantic"][c], porCat["contextual"][c])
	}
	linha("TOTAL", total["semantic"], total["contextual"])
}
