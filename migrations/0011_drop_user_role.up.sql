-- 0011_drop_user_role: remove the unused role column from users.
-- No index, constraint, or enum ever referenced users.role (plain VARCHAR),
-- and no route gated on it, so dropping the column is sufficient.

ALTER TABLE IF EXISTS users DROP COLUMN IF EXISTS role;
