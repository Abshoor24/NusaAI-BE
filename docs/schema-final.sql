-- ==============================================================================
-- SKEMA FINAL DATABASE: AI Website Generator (Go + PostgreSQL 17)
-- ==============================================================================
-- DDL ini mencerminkan skema database final setelah seluruh migrasi (0001-0012)
-- diterapkan. Seluruh modul legacy (chatbot, RAG, code execution) telah dibersihkan.
--
-- Karakteristik Skema:
-- 1. Tabel Inti Aplikasi: users, projects, project_versions, project_attachments
-- 2. Konvensi Kunci: Primary Key bertipe UUID (gen_random_uuid)
-- 3. Integritas Relasional: Seluruh relasi 1:N menggunakan ON DELETE CASCADE
-- 4. Audit Trail: created_at dan updated_at menggunakan TIMESTAMPTZ DEFAULT now()
-- ==============================================================================

-- ------------------------------------------------------------------------------
-- EXTENSIONS
-- ------------------------------------------------------------------------------
-- pgcrypto: Diperlukan untuk fungsi gen_random_uuid() dan fungsi kriptografi.
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

-- vector: Diaktifkan pada migrasi 0002/0006. Tidak lagi dipakai oleh tabel aktif,
-- namun dipertahankan apabila modul RAG/vektor diaktifkan kembali.
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public;

-- ------------------------------------------------------------------------------
-- TABEL: users
-- ------------------------------------------------------------------------------
-- Menyimpan kredensial autentikasi pengguna aplikasi (fondasi register & login).
-- Catatan: Kolom 'role' telah dihapus pada migrasi 0011 (tidak digunakan).
-- Kolom 'id' bertipe UUID tanpa default di DB karena di-generate di layer aplikasi.
-- ------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.users (
    id            UUID                     NOT NULL,
    email         VARCHAR(255)             NOT NULL,
    password_hash TEXT                     NOT NULL,
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT users_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON public.users USING btree (email);

-- ------------------------------------------------------------------------------
-- TABEL: projects
-- ------------------------------------------------------------------------------
-- Entitas utama generator website. Menyimpan brief pembuatan website (JSONB),
-- status proses AI generation, serta kepemilikan oleh pengguna.
-- ------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.projects (
    id         UUID                     NOT NULL DEFAULT gen_random_uuid(),
    user_id    UUID                     NOT NULL,
    title      VARCHAR(200)             NOT NULL,
    status     VARCHAR(20)              NOT NULL DEFAULT 'draft'::character varying,
    brief      JSONB                    NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT projects_pkey PRIMARY KEY (id),
    CONSTRAINT projects_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT chk_projects_status CHECK (status::text = ANY (ARRAY['draft'::character varying, 'generating'::character varying, 'ready'::character varying, 'failed'::character varying]::text[]))
);

CREATE INDEX IF NOT EXISTS idx_projects_user_updated ON public.projects USING btree (user_id, updated_at DESC);

-- ------------------------------------------------------------------------------
-- TABEL: project_versions
-- ------------------------------------------------------------------------------
-- Riwayat iterasi dokumen HTML yang dihasilkan oleh model AI (generate atau revise).
-- Nomor versi bertambah secara berurutan untuk tiap project_id.
-- ------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.project_versions (
    id             UUID                     NOT NULL DEFAULT gen_random_uuid(),
    project_id     UUID                     NOT NULL,
    version_number INTEGER                  NOT NULL,
    kind           VARCHAR(20)              NOT NULL,
    prompt         TEXT                     NOT NULL DEFAULT ''::text,
    html           TEXT                     NOT NULL,
    created_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT project_versions_pkey PRIMARY KEY (id),
    CONSTRAINT project_versions_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT uq_project_versions_project_number UNIQUE (project_id, version_number),
    CONSTRAINT chk_project_versions_kind CHECK (kind::text = ANY (ARRAY['generate'::character varying, 'revise'::character varying]::text[])),
    CONSTRAINT chk_project_versions_number CHECK (version_number >= 1)
);

-- ------------------------------------------------------------------------------
-- TABEL: project_attachments
-- ------------------------------------------------------------------------------
-- Metadata berkas gambar referensi yang diunggah pengguna untuk sebuah project.
-- Berkas fisik disimpan di sistem penyimpanan lokal (uploads/{user_id}/{project_id}/...).
-- ------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.project_attachments (
    id           UUID                     NOT NULL DEFAULT gen_random_uuid(),
    project_id   UUID                     NOT NULL,
    file_name    VARCHAR(255)             NOT NULL,
    file_type    VARCHAR(20)              NOT NULL,
    file_size    BIGINT                   NOT NULL,
    storage_path TEXT                     NOT NULL,
    mime_type    VARCHAR(100)             NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT project_attachments_pkey PRIMARY KEY (id),
    CONSTRAINT project_attachments_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT chk_project_attachments_size CHECK (file_size >= 0)
);

CREATE INDEX IF NOT EXISTS idx_project_attachments_project_created ON public.project_attachments USING btree (project_id, created_at DESC);
