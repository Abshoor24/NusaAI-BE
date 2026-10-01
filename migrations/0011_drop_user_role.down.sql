-- 0011_drop_user_role rollback: recreate the role column.
-- NOTE: the original per-row role values are unrecoverable after the up
-- migration drops the column; this only restores the old shape
-- (NOT NULL with the previous 'admin' default).

ALTER TABLE IF EXISTS users
    ADD COLUMN IF NOT EXISTS role VARCHAR(32) NOT NULL DEFAULT 'admin';
