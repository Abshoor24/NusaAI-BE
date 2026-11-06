-- 0012_drop_legacy_tables (up): remove the unused chatbot, RAG, and code
-- execution tables left over from earlier product iterations.
--
-- WARNING: DATA LOSS. This migration permanently deletes every row in the
-- six legacy tables below. Take a backup before applying it to production.
-- The down migration recreates empty tables only; it cannot restore data.
--
-- These tables are not used by the AI Website Generator. Their handlers were
-- removed from the router (see internal/routes/routes.go) and the
-- corresponding Go packages have been deleted.
--
-- Active tables are only: users, projects, project_versions,
-- project_attachments.
--
-- Children are dropped before parents and no CASCADE is used, so an
-- unexpected dependency fails the migration instead of silently dropping
-- more tables. The whole migration runs in one transaction.

BEGIN;

-- Chat history: attachments -> messages -> conversations.
DROP TABLE IF EXISTS message_attachments;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;

-- RAG knowledge base: chunks -> documents.
DROP TABLE IF EXISTS document_chunks;
DROP TABLE IF EXISTS documents;

-- Code execution sandbox history.
DROP TABLE IF EXISTS code_executions;

COMMIT;
