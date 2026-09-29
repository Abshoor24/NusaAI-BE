-- 0010_projects: AI Website Generator (Tahap A) — projects, versions, attachments.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(200) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    brief JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_projects_status CHECK (status IN ('draft', 'generating', 'ready', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_projects_user_updated ON projects (user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS project_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_number INT NOT NULL,
    kind VARCHAR(20) NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    html TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_project_versions_kind CHECK (kind IN ('generate', 'revise')),
    CONSTRAINT chk_project_versions_number CHECK (version_number >= 1),
    CONSTRAINT uq_project_versions_project_number UNIQUE (project_id, version_number)
);

CREATE TABLE IF NOT EXISTS project_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_name VARCHAR(255) NOT NULL,
    file_type VARCHAR(20) NOT NULL,
    file_size BIGINT NOT NULL,
    storage_path TEXT NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_project_attachments_size CHECK (file_size >= 0)
);

CREATE INDEX IF NOT EXISTS idx_project_attachments_project_created ON project_attachments (project_id, created_at DESC);
