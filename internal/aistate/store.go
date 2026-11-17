// Package aistate persists cross-request AI state that must survive a backend
// restart: the daily quota counter/exhaustion marker and the exact-brief HTML
// cache. Nothing here calls a model.
package aistate

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QuotaRecord is one model's state for a quota window.
type QuotaRecord struct {
	Model          string
	GenerateCount  int
	ExhaustedUntil time.Time
}

// querier is the subset of pgxpool.Pool used by Store. Keeping it small lets
// tests exercise the SQL semantics without a live Postgres instance.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is the Postgres-backed implementation of the quota/cache stores.
type Store struct {
	db querier
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{db: pool}
}

func windowDate(windowStart time.Time) string {
	return windowStart.Format("2006-01-02")
}

// LoadQuota returns every model row for the given quota window. Rows from
// previous windows are intentionally ignored (the 15:00 WIB reset).
func (s *Store) LoadQuota(ctx context.Context, windowStart time.Time) ([]QuotaRecord, error) {
	const q = `SELECT model, generate_count, exhausted_until
		FROM ai_quota_usage WHERE window_date = $1::date`
	rows, err := s.db.Query(ctx, q, windowDate(windowStart))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]QuotaRecord, 0, 4)
	for rows.Next() {
		var rec QuotaRecord
		var exhausted *time.Time
		if err := rows.Scan(&rec.Model, &rec.GenerateCount, &exhausted); err != nil {
			return nil, err
		}
		if exhausted != nil {
			rec.ExhaustedUntil = *exhausted
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// IncrementGenerate adds one provider generate call for model in the window.
func (s *Store) IncrementGenerate(ctx context.Context, model string, windowStart time.Time) error {
	const q = `INSERT INTO ai_quota_usage (model, window_date, generate_count, updated_at)
		VALUES ($1, $2::date, 1, NOW())
		ON CONFLICT (model, window_date) DO UPDATE
		SET generate_count = ai_quota_usage.generate_count + 1, updated_at = NOW()`
	_, err := s.db.Exec(ctx, q, model, windowDate(windowStart))
	return err
}

// MarkExhausted persists the daily-quota cooldown for model in the window.
func (s *Store) MarkExhausted(ctx context.Context, model string, windowStart time.Time, until time.Time) error {
	const q = `INSERT INTO ai_quota_usage (model, window_date, generate_count, exhausted_until, updated_at)
		VALUES ($1, $2::date, 0, $3, NOW())
		ON CONFLICT (model, window_date) DO UPDATE
		SET exhausted_until = GREATEST(COALESCE(ai_quota_usage.exhausted_until, 'epoch'::timestamptz), EXCLUDED.exhausted_until),
		    updated_at = NOW()`
	_, err := s.db.Exec(ctx, q, model, windowDate(windowStart), until)
	return err
}

// GetHTML returns cached validated HTML for an exact brief hash.
func (s *Store) GetHTML(ctx context.Context, hash string) (string, bool, error) {
	const q = `SELECT html FROM ai_html_cache WHERE brief_hash = $1`
	var html string
	err := s.db.QueryRow(ctx, q, hash).Scan(&html)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return html, true, nil
}

// PutHTML stores validated HTML for an exact brief hash.
func (s *Store) PutHTML(ctx context.Context, hash, html string) error {
	const q = `INSERT INTO ai_html_cache (brief_hash, html) VALUES ($1, $2)
		ON CONFLICT (brief_hash) DO UPDATE SET html = EXCLUDED.html`
	_, err := s.db.Exec(ctx, q, hash, html)
	return err
}
