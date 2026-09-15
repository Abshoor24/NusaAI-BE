-- 0002_company_profile_rag: enable pgvector and create company_documents table for RAG.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS company_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id VARCHAR(255) NOT NULL,
    content TEXT NOT NULL,
    embedding VECTOR(768),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_company_documents_company_id ON company_documents (company_id);
