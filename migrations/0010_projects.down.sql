-- 0010_projects rollback: drop website-generator tables (newest first).

DROP TABLE IF EXISTS project_attachments;
DROP TABLE IF EXISTS project_versions;
DROP TABLE IF EXISTS projects;
