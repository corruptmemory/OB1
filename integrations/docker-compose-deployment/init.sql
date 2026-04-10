-- Open Brain schema for self-hosted PostgreSQL + pgvector (docker-compose edition).
-- This is a fork of integrations/kubernetes-deployment/k8s/init.sql with the vector
-- dimension changed from 1536 to 1024 to match Ollama's mxbai-embed-large output.
-- If you switch to a different embedding model, update both vector() declarations
-- below AND rebuild the table (the column type cannot be altered with existing data).

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS thoughts (
    id BIGSERIAL PRIMARY KEY,
    content TEXT NOT NULL,
    embedding vector(1024),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_thoughts_created_at ON thoughts (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_thoughts_metadata ON thoughts USING GIN (metadata);

-- match_thoughts function for vector similarity search.
-- Kept for parity with the Supabase schema — the MCP server uses raw SQL via
-- the pg connection pool and does not call this function, but other clients
-- (dashboards, recipes, ad-hoc psql sessions) may.
CREATE OR REPLACE FUNCTION match_thoughts(
    query_embedding vector(1024),
    match_threshold FLOAT DEFAULT 0.5,
    match_count INT DEFAULT 10,
    filter JSONB DEFAULT '{}'::jsonb
)
RETURNS TABLE (
    id BIGINT,
    content TEXT,
    metadata JSONB,
    similarity FLOAT,
    created_at TIMESTAMP WITH TIME ZONE
)
LANGUAGE plpgsql
AS $$
BEGIN
    RETURN QUERY
    SELECT
        t.id,
        t.content,
        t.metadata,
        (1 - (t.embedding <=> query_embedding))::FLOAT AS similarity,
        t.created_at
    FROM thoughts t
    WHERE 1 - (t.embedding <=> query_embedding) >= match_threshold
    ORDER BY t.embedding <=> query_embedding
    LIMIT match_count;
END;
$$;
