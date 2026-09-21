-- 0009_code_executions: Code execution sandbox history table.

CREATE TABLE IF NOT EXISTS code_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    language VARCHAR(50) NOT NULL,
    code TEXT NOT NULL,
    stdin TEXT,
    stdout TEXT NOT NULL DEFAULT '',
    stderr TEXT NOT NULL DEFAULT '',
    exit_code INT NOT NULL DEFAULT 0,
    execution_time_ms BIGINT NOT NULL DEFAULT 0,
    memory_kb BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_code_executions_user_id ON code_executions (user_id);
CREATE INDEX IF NOT EXISTS idx_code_executions_created_at ON code_executions (created_at DESC);
