package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// repository guarda o pool e expõe as operações no banco.
type Repository struct {
	db *pgxpool.Pool
}

// New reccebe o pool já pronto e devolve o repository
func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// Chunk é o que entra na tabela: texto + metadados + embedding
type Chunk struct {
	Documento string
	Secao     string
	Pagina    int
	Ordem     int
	Conteudo  string
	Tokens    int
	Embedding []float32 //1024 floats (bge-m3)
}

// Insert grava um chunk. pgvector.NewVector converte []float32 no tipo 'vector'
// (Só funciona porque o AfterConnect registrou o tipo na conexão)
func (r *Repository) Insert(ctx context.Context, c Chunk) error {
	_, err := r.db.Exec(ctx, `
	INSERT INTO chunks (documento, secao, pagina, ordem, conteudo, tokens, embedding)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.Documento, c.Secao, c.Pagina, c.Ordem, c.Conteudo, c.Tokens, pgvector.NewVector(c.Embedding))
	if err != nil {
		return fmt.Errorf("failed to insert chunk: %w", err)
	}
	return nil
}

// DeleteByDocumento remove todos os chunks de um documento (re-ingestão
// idempotente). A FK de chunks_contexto tem ON DELETE CASCADE, então o
// contexto/embedding contextual daquele documento cai junto — e precisará
// ser regenerado (ctx export/import/backfill) após a re-ingestão.
func (r *Repository) DeleteByDocumento(ctx context.Context, documento string) (int64, error) {
	tag, err := r.db.Exec(ctx, "DELETE FROM chunks WHERE documento = $1", documento)
	if err != nil {
		return 0, fmt.Errorf("failed to delete chunks of %s: %w", documento, err)
	}
	return tag.RowsAffected(), nil
}

// --- Contextual retrieval (tabela chunks_contexto) ---

// InsertContexto grava/atualiza o contexto textual de um chunk (embedding_ctx fica NULL ate o backfill).
func (r *Repository) InsertContexto(ctx context.Context, chunkID int64, contexto, modelo string) error {
	_, err := r.db.Exec(ctx, `
	INSERT INTO chunks_contexto (chunk_id, contexto, modelo_ctx)
	VALUES ($1, $2, $3)
	ON CONFLICT (chunk_id) DO UPDATE SET contexto = EXCLUDED.contexto, modelo_ctx = EXCLUDED.modelo_ctx, embedding_ctx = NULL`,
		chunkID, contexto, modelo)
	if err != nil {
		return fmt.Errorf("insert contexto chunk %d: %w", chunkID, err)
	}
	return nil
}

// ChunkPendente é um chunk que tem contexto mas ainda nao tem embedding_ctx.
type ChunkPendente struct {
	ChunkID  int64
	Conteudo string
	Contexto string
}

// ListContextoPendente devolve os chunks com contexto e embedding_ctx ainda NULL.
func (r *Repository) ListContextoPendente(ctx context.Context) ([]ChunkPendente, error) {
	rows, err := r.db.Query(ctx, `
	SELECT cc.chunk_id, c.conteudo, cc.contexto
	FROM chunks_contexto cc JOIN chunks c ON c.id = cc.chunk_id
	WHERE cc.embedding_ctx IS NULL
	ORDER BY cc.chunk_id`)
	if err != nil {
		return nil, fmt.Errorf("listar pendentes: %w", err)
	}
	defer rows.Close()
	var out []ChunkPendente
	for rows.Next() {
		var p ChunkPendente
		if err := rows.Scan(&p.ChunkID, &p.Conteudo, &p.Contexto); err != nil {
			return nil, fmt.Errorf("scan pendente: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateEmbeddingCtx grava o vetor contextual de um chunk.
func (r *Repository) UpdateEmbeddingCtx(ctx context.Context, chunkID int64, emb []float32) error {
	_, err := r.db.Exec(ctx, `UPDATE chunks_contexto SET embedding_ctx = $1 WHERE chunk_id = $2`,
		pgvector.NewVector(emb), chunkID)
	if err != nil {
		return fmt.Errorf("update embedding_ctx chunk %d: %w", chunkID, err)
	}
	return nil
}

// SearchCtx é a busca contextual: ordena por embedding_ctx (contexto+conteudo).
// Devolve o MESMO Resultado (com ID e o conteudo ORIGINAL — o contexto serve so p/ achar).
func (r *Repository) SearchCtx(ctx context.Context, queryEmb []float32, k int) ([]Resultado, error) {
	rows, err := r.db.Query(ctx, `SELECT c.id, c.documento, c.secao, c.conteudo, 1 - (cc.embedding_ctx <=> $1) AS similaridade
	FROM chunks_contexto cc JOIN chunks c ON c.id = cc.chunk_id
	WHERE cc.embedding_ctx IS NOT NULL
	ORDER BY cc.embedding_ctx <=> $1
	LIMIT $2`,
		pgvector.NewVector(queryEmb), k)
	if err != nil {
		return nil, fmt.Errorf("failed to execute searchctx query: %w", err)
	}
	defer rows.Close()
	var out []Resultado
	for rows.Next() {
		var res Resultado
		if err := rows.Scan(&res.ID, &res.Documento, &res.Secao, &res.Conteudo, &res.Similaridade); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// Search recebe um embedding e retorna os mais similares(0..1).
type Resultado struct {
	ID           int64 // id do chunk no banco (rastreabilidade p/ recall@k)
	Documento    string
	Secao        string
	Conteudo     string
	Similaridade float64
}

// Search devolve os k chunks mais próximos da pergunta (distância de cosseno <=>)
func (r *Repository) Search(ctx context.Context, queryEmb []float32, k int) ([]Resultado, error) {
	rows, err := r.db.Query(ctx, `SELECT id, documento, secao, conteudo, 1  - (embedding <=> $1) AS similaridade
	FROM chunks
	ORDER BY embedding <=> $1
	LIMIT $2`,
		pgvector.NewVector(queryEmb), k)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search query: %w", err)
	}
	defer rows.Close() //fechando query para libera conexão

	var out []Resultado
	for rows.Next() {
		var res Resultado
		if err := rows.Scan(&res.ID, &res.Documento, &res.Secao, &res.Conteudo, &res.Similaridade); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		out = append(out, res)
	}
	return out, rows.Err() // error que ode ter ocrrido durante a iteração
}
