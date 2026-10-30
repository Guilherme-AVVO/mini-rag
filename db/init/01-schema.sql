-- Executado automaticamente na PRIMEIRA subida do container
-- (quando o diretório de dados está vazio). Em subidas seguintes NÃO roda de novo.

-- 1) Habilita a extensão pgvector
CREATE EXTENSION IF NOT EXISTS vector;

-- 2) Tabela de chunks do RAG (dimensão 1024 = embedding do bge-m3)
CREATE TABLE IF NOT EXISTS chunks (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    documento TEXT        NOT NULL,   -- 'RBAC 154 EMD08'
    secao     TEXT,                   -- '5.3.9' (citação fina; pode ser NULL)
    pagina    INT,                    -- rastreabilidade até o PDF
    ordem     INT,                    -- posição do chunk no documento
    conteudo  TEXT        NOT NULL,   -- TEXTO original do chunk (vai para o LLM)
    tokens    INT,                    -- tamanho do chunk em tokens
    embedding VECTOR(1024) NOT NULL   -- vetor do bge-m3 (usado só na busca)
);
