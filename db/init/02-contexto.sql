-- Contextual retrieval: tabela paralela ligada por chunk_id.
-- Nao toca em chunks (baseline) — preserva embedding original e os ids do gabarito.
CREATE TABLE IF NOT EXISTS chunks_contexto (
    chunk_id      BIGINT PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    contexto      TEXT NOT NULL,          -- prefixo gerado por LLM (situa o chunk no documento)
    embedding_ctx VECTOR(1024),           -- bge-m3 de (contexto + '\n\n' + conteudo); NULL ate o backfill
    modelo_ctx    TEXT,                   -- rastreabilidade (qual LLM gerou o contexto)
    criado_em     TIMESTAMPTZ DEFAULT now()
);
