DROP INDEX IF EXISTS idx_documents_status;

ALTER TABLE documents 
    DROP COLUMN IF EXISTS file_name,
    DROP COLUMN IF EXISTS file_type,
    DROP COLUMN IF EXISTS file_size,
    DROP COLUMN IF EXISTS file_path,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS error_message;
