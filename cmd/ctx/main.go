// cmd/ctx: ferramenta unica do pipeline de contextual retrieval.
// Subcomandos:
//
//	export    exporta os chunks do banco por documento -> ctx_work/<doc>.json
//	import    importa ctx_work/contextos.json -> tabela chunks_contexto
//	backfill  re-embeda (contexto + conteudo) com bge-m3 -> embedding_ctx
//	all       roda import + backfill (pressupoe contextos.json ja gerado)
//
// Uso: go run ./cmd/ctx <subcomando>
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Guilherme-AVVO/mini-rag/internal/config"
	"github.com/Guilherme-AVVO/mini-rag/internal/database"
	"github.com/Guilherme-AVVO/mini-rag/internal/embed"
	"github.com/Guilherme-AVVO/mini-rag/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ctxDir = "ctx_work"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()
	cfg := config.LoadConfig()
	pool, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("conectar ao banco: %v", err)
	}
	defer pool.Close()
	repo := repository.New(pool)

	switch os.Args[1] {
	case "export":
		doExport(ctx, pool)
	case "import":
		doImport(ctx, repo)
	case "backfill":
		doBackfill(ctx, repo, cfg)
	case "all":
		doImport(ctx, repo)
		doBackfill(ctx, repo, cfg)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "uso: ctx <export|import|backfill|all>")
	os.Exit(2)
}

// ---------- export ----------

type chunkExport struct {
	ID       int64  `json:"id"`
	Secao    string `json:"secao"`
	Ordem    int    `json:"ordem"`
	Conteudo string `json:"conteudo"`
}
type docExport struct {
	Documento string        `json:"documento"`
	Chunks    []chunkExport `json:"chunks"`
}

func doExport(ctx context.Context, pool *pgxpool.Pool) {
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		log.Fatalf("criar %s: %v", ctxDir, err)
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT documento FROM chunks ORDER BY documento`)
	if err != nil {
		log.Fatalf("listar documentos: %v", err)
	}
	var docs []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			log.Fatalf("scan documento: %v", err)
		}
		docs = append(docs, d)
	}
	rows.Close()

	total := 0
	var todos []docExport
	for _, doc := range docs {
		r, err := pool.Query(ctx,
			`SELECT id, COALESCE(secao,''), COALESCE(ordem,0), conteudo
			 FROM chunks WHERE documento=$1 ORDER BY ordem, id`, doc)
		if err != nil {
			log.Fatalf("query chunks de %s: %v", doc, err)
		}
		de := docExport{Documento: doc}
		for r.Next() {
			var c chunkExport
			if err := r.Scan(&c.ID, &c.Secao, &c.Ordem, &c.Conteudo); err != nil {
				log.Fatalf("scan chunk: %v", err)
			}
			de.Chunks = append(de.Chunks, c)
		}
		r.Close()
		b, _ := json.MarshalIndent(de, "", "  ")
		os.WriteFile(filepath.Join(ctxDir, doc+".json"), b, 0o644)
		todos = append(todos, de)
		total += len(de.Chunks)
		log.Printf("  %-40s %d chunks", doc, len(de.Chunks))
	}
	b, _ := json.MarshalIndent(todos, "", "  ")
	os.WriteFile(filepath.Join(ctxDir, "_todos.json"), b, 0o644)
	log.Printf("export: %d chunks de %d documentos em %s/", total, len(docs), ctxDir)
}

// ---------- import ----------

type ctxItem struct {
	ID        int64  `json:"id"`
	Contexto  string `json:"contexto"`
	ModeloCtx string `json:"modelo_ctx"`
}

func doImport(ctx context.Context, repo *repository.Repository) {
	path := filepath.Join(ctxDir, "contextos.json")
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("ler %s: %v", path, err)
	}
	var itens []ctxItem
	if err := json.Unmarshal(b, &itens); err != nil {
		log.Fatalf("parse %s: %v", path, err)
	}
	n := 0
	for _, it := range itens {
		if it.Contexto == "" {
			log.Fatalf("contexto vazio para chunk %d", it.ID)
		}
		if err := repo.InsertContexto(ctx, it.ID, it.Contexto, it.ModeloCtx); err != nil {
			log.Fatalf("import chunk %d: %v", it.ID, err)
		}
		n++
	}
	log.Printf("import: %d contextos gravados em chunks_contexto", n)
}

// ---------- backfill ----------

func doBackfill(ctx context.Context, repo *repository.Repository, cfg *config.Config) {
	emb, err := embed.New(cfg.LLM_URL, cfg.EMBED_MODEL)
	if err != nil {
		log.Fatalf("criar embedder: %v", err)
	}
	pendentes, err := repo.ListContextoPendente(ctx)
	if err != nil {
		log.Fatalf("listar pendentes: %v", err)
	}
	if len(pendentes) == 0 {
		log.Printf("backfill: nada a fazer (nenhum embedding_ctx pendente)")
		return
	}
	inicio := time.Now()
	for i, p := range pendentes {
		texto := p.Contexto + "\n\n" + p.Conteudo // junta TEXTO -> 1 vetor 1024-dim
		vec, err := emb.Embed(ctx, texto)
		if err != nil {
			log.Fatalf("embed chunk %d: %v", p.ChunkID, err)
		}
		if err := repo.UpdateEmbeddingCtx(ctx, p.ChunkID, vec); err != nil {
			log.Fatalf("gravar chunk %d: %v", p.ChunkID, err)
		}
		if (i+1)%10 == 0 || i+1 == len(pendentes) {
			log.Printf("  %d/%d", i+1, len(pendentes))
		}
	}
	log.Printf("backfill: %d chunks re-embedados em %s", len(pendentes), time.Since(inicio).Round(time.Millisecond))
}
