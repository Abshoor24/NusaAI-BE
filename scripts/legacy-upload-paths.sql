-- Read-only export of files referenced by the legacy chatbot/RAG tables.
--
-- RUN THIS BEFORE migrations/0012_drop_legacy_tables.up.sql. After 0012 the
-- message_attachments and documents tables no longer exist.
--
-- This file only reads data. It never writes to the database and never
-- touches the filesystem. Do NOT point DATABASE_URL at production unless
-- you have explicitly been asked to audit production.
--
-- Usage (from the repository root, bash):
--   psql "$DATABASE_URL" -X -q -v ON_ERROR_STOP=1 \
--     -A -t -F $'\t' -f scripts/legacy-upload-paths.sql \
--     > legacy-upload-paths.tsv
--
-- Output columns:
--   source | id | user_id | path | file_name | file_size
--
-- Path notes:
--   - message_attachments.storage_path is created as
--     <UPLOAD_DIR>/<user_id>/<attachment_id>_<file_name>.
--   - documents.file_path is created as
--     <UPLOAD_DIR>/<user_id>/<document_id>_<file_name>.
--   - Both were written relative to the server process working directory
--     when UPLOAD_DIR was relative (the default is "uploads").
--   - project_attachments.storage_path is NOT included here because those
--     files are still active. The dry-run script reads that table separately
--     and refuses to list an active file for deletion.

BEGIN TRANSACTION READ ONLY;

SELECT
    'message_attachments' AS source,
    ma.id::text           AS id,
    c.user_id::text       AS user_id,
    ma.storage_path       AS path,
    ma.file_name          AS file_name,
    ma.file_size          AS file_size
FROM message_attachments ma
JOIN messages m      ON m.id = ma.message_id
JOIN conversations c ON c.id = m.conversation_id

UNION ALL

SELECT
    'documents'                    AS source,
    d.id::text                     AS id,
    d.user_id::text                AS user_id,
    COALESCE(d.file_path, '')      AS path,
    COALESCE(d.file_name, '')      AS file_name,
    d.file_size                    AS file_size
FROM documents d

ORDER BY 1, 2;

COMMIT;
