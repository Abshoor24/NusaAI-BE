-- 0001_init: infrastructure tables only.
--
-- IMPORTANT: no business tables (products, orders, services, projects,
-- customers, reservations, ...) are created here because the client is not
-- known yet. The users table below exists solely to support the
-- authentication foundation (register/login/me).
--
-- TODO: CLIENT REQUIREMENT - add business schema only after client
-- requirements are gathered.

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY,
    email         VARCHAR(255) NOT NULL,
    password_hash TEXT         NOT NULL,
    role          VARCHAR(32)  NOT NULL DEFAULT 'admin',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Emails must be unique so each user has exactly one account.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_idx ON users (email);
