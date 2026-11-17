-- 0013_ai_quota_cache: persistent AI quota markers/counters and HTML cache.
--
-- Quota rows are keyed by the free-tier quota day, which resets at 15:00 WIB
-- (midnight Pacific). A new window creates a new row, so old windows expire
-- automatically without a scheduled job.
CREATE TABLE IF NOT EXISTS ai_quota_usage (
    model VARCHAR(120) NOT NULL,
    window_date DATE NOT NULL,
    generate_count INT NOT NULL DEFAULT 0,
    exhausted_until TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (model, window_date),
    CONSTRAINT chk_ai_quota_generate_count CHECK (generate_count >= 0)
);

-- Exact-brief cache: only validated HTML is stored. The hash is SHA-256 of
-- the canonical brief JSON, so identical briefs reuse the result without
-- calling the model (and without consuming quota).
CREATE TABLE IF NOT EXISTS ai_html_cache (
    brief_hash CHAR(64) PRIMARY KEY,
    html TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
