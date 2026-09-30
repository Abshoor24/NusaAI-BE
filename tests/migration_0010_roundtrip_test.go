// Migration 0010 round-trip on the CURRENT (dummy-data) database.
//
// This test proves that 0010 goes down and back up on the database that
// DATABASE_URL points at. It DELETES the projects/project_versions/
// project_attachments tables (and their rows) in the middle of the run,
// so it only runs with explicit opt-in:
//
//	MIGRATION_ROUNDTRIP_CONFIRM=saya-paham-data-dummy go test ./tests/ -run Roundtrip -v -count=1
//
// Without the exact confirm value the test skips cleanly, so plain
// `go test ./...` stays green.
//
// Safety rules (all enforced in code, none bypassable):
//   - Only version 10 -> 9 -> 10. Downgrading below 9 is never attempted.
//   - Connection comes from DATABASE_URL (env, or repo-root .env loaded the
//     same way config.go does it: never overriding existing env vars).
//   - Port 6543 (transaction pooler) and non-TLS sslmode are refused.
//     A missing sslmode is upgraded to require for the test connection.
//   - schema_migrations must read exactly version=10, dirty=false;
//     anything else aborts with no automatic repair.
//   - Row counts of the 0010 tables + users are printed BEFORE anything
//     is dropped, so the data loss is visible.
//   - Pool is capped at 3 connections; connect timeout 15s; the whole test
//     runs under a 3-minute deadline (migrate library calls themselves take
//     no context — a documented limitation, see below).
//   - t.Cleanup always tries to leave the database at version 10. If the
//     database is dirty, NOTHING is forced; manual recovery steps are
//     printed instead (see README "Verifikasi migrasi 0010").
//
// Passwords and full URLs are never printed or logged; only host, port,
// dbname and user.
package tests

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/database"
)

// roundtripConfirm is the exact opt-in value. Compared with ==, never logged.
const roundtripConfirm = "saya-paham-data-dummy"

// Tables created by migration 0010. Used for row counts, absence checks,
// and filtering snapshot lines. Kept in one place so a schema change only
// needs a single edit.
var roundtripTables0010 = []string{"projects", "project_versions", "project_attachments"}

// roundtripDatabaseURL resolves DATABASE_URL exactly like config.go does:
// process environment first, then repo-root .env (development only) without
// overriding anything already set. Returns "" when not found anywhere.
func roundtripDatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	if env := os.Getenv("APP_ENV"); env != "" && env != "development" {
		return ""
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	envPath := filepath.Join(filepath.Dir(thisFile), "..", ".env")
	if _, err := os.Stat(envPath); err != nil {
		return ""
	}
	_ = godotenv.Load(envPath) // same as config.go: never overrides env
	return os.Getenv("DATABASE_URL")
}

// roundtripIdentity parses, guards, and normalizes the DSN. It prints only
// host, port, dbname and user — never the password or the full URL.
func roundtripIdentity(t *testing.T, dsn string) (url.URL, string, string, string, string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DATABASE_URL is not a valid URL: %v", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatalf("DATABASE_URL must use a postgres/postgresql scheme, got %q", u.Scheme)
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	if port == "6543" {
		t.Fatalf("refusing to run: port 6543 is the Supabase transaction pooler (DDL/migrations are unreliable there); use the Session pooler (port 5432)")
	}
	q := u.Query()
	sslmode := strings.ToLower(q.Get("sslmode"))
	if sslmode == "" {
		sslmode = "require"
		q.Set("sslmode", "require")
		u.RawQuery = q.Encode()
		t.Logf("DATABASE_URL has no sslmode; using sslmode=require for the test connection")
	}
	switch sslmode {
	case "require", "verify-ca", "verify-full":
		// ok
	default:
		t.Fatalf("refusing to run: sslmode must be require/verify-ca/verify-full, got %q", sslmode)
	}
	user := "(none)"
	if u.User != nil {
		user = u.User.Username()
	}
	dbname := strings.TrimPrefix(u.Path, "/")
	t.Logf("roundtrip database identity: host=%s port=%s dbname=%s user=%s sslmode=%s", u.Hostname(), port, dbname, user, sslmode)
	return *u, port, dbname, user, u.String()
}

// readMigrationVersion reads golang-migrate bookkeeping. version is the
// applied migration number, dirty reports an interrupted migration.
func readMigrationVersion(ctx context.Context, pool *pgxpool.Pool) (int64, bool, error) {
	var version int64
	var dirty bool
	err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty)
	if err != nil {
		return 0, false, err
	}
	return version, dirty, nil
}

// countRows returns row counts for tables that exist; missing tables count
// as -1 so absence is distinguishable from emptiness.
func countRows(ctx context.Context, pool *pgxpool.Pool, tables []string) map[string]int64 {
	out := make(map[string]int64, len(tables))
	for _, tbl := range tables {
		var n int64 = -1
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+tbl).Scan(&n)
		out[tbl] = n
	}
	return out
}

// without0010Objects drops every snapshot line that belongs to a 0010
// object (table, its columns, its indexes incl. auto-created pkey/unique
// indexes, its constraints). Other migrations' objects never mention these
// table names, so the filter is exact.
func without0010Objects(snapshot string) string {
	owned := map[string]bool{
		"projects": true, "project_versions": true, "project_attachments": true,
		"idx_projects_user_updated": true, "idx_project_attachments_project_created": true,
	}
	var kept []string
	for _, line := range strings.Split(snapshot, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		drop := false
		switch parts[0] {
		case "table", "column", "constraint":
			if len(parts) > 1 && owned[strings.TrimSpace(parts[1])] {
				drop = true
			}
		case "index":
			if len(parts) > 1 && owned[strings.TrimSpace(parts[1])] {
				drop = true
			}
			if !drop && len(parts) > 2 {
				def := parts[2]
				if strings.Contains(def, "public.projects") ||
					strings.Contains(def, "public.project_versions") ||
					strings.Contains(def, "public.project_attachments") {
					drop = true
				}
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n") + "\n"
}

func TestMigration0010Roundtrip_DummyDB(t *testing.T) {
	if os.Getenv("MIGRATION_ROUNDTRIP_CONFIRM") != roundtripConfirm {
		t.Skip("set MIGRATION_ROUNDTRIP_CONFIRM=saya-paham-data-dummy to run the 0010 round-trip test on the DATABASE_URL database (it drops and recreates the 0010 tables)")
	}

	dsn := roundtripDatabaseURL()
	if dsn == "" {
		t.Fatal("DATABASE_URL not found in environment or repo-root .env; nothing to connect to")
	}
	_, _, _, _, normalized := roundtripIdentity(t, dsn)

	cfg := config.DBConfig{URL: normalized, MaxConns: 3, ConnectTimeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(database.DSN(cfg))
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	poolCfg.ConnConfig.ConnectTimeout = 15 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	// Guard: must be exactly version 10, not dirty. Never auto-repair.
	version, dirty, err := readMigrationVersion(ctx, pool)
	if err != nil {
		t.Fatalf("cannot read schema_migrations (is this a migrated database?): %v", err)
	}
	t.Logf("schema_migrations before test: version=%d dirty=%v", version, dirty)
	if dirty || version != 10 {
		t.Fatalf("refusing to run: need version=10 dirty=false, got version=%d dirty=%v; inspect manually, do not force", version, dirty)
	}

	// Show what is about to be dropped.
	counts := countRows(ctx, pool, append(append([]string{}, roundtripTables0010...), "users"))
	for _, tbl := range append(append([]string{}, roundtripTables0010...), "users") {
		n := counts[tbl]
		if n < 0 {
			t.Logf("row count %s: table missing", tbl)
		} else {
			t.Logf("row count %s: %d (will be lost on down, except users)", tbl, n)
		}
	}

	// Cleanup: whatever happens, try to leave the DB at version 10.
	// Dirty state is NEVER forced; manual steps are printed instead.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), testConnTimeout)
		defer ccancel()
		v, d, err := readMigrationVersion(cctx, pool)
		if err != nil {
			t.Errorf("cleanup: cannot read schema_migrations (%v). Manual recovery: run SELECT * FROM schema_migrations; then follow the README recovery procedure", err)
			return
		}
		switch {
		case d:
			t.Errorf("cleanup: database is dirty at version %d; NOT forcing repair. Manual recovery: 1) SELECT * FROM schema_migrations; 2) check which of projects/project_versions/project_attachments exist; 3) only after the schema matches, UPDATE schema_migrations SET version=<correct>, dirty=false; 4) re-run this test", v)
		case v == 9:
			if err := database.MigrateTo(config.DBConfig{URL: normalized}, 10); err != nil {
				t.Errorf("cleanup: MigrateTo(10) failed: %v. Manual recovery: inspect schema_migrations and follow the README recovery procedure", err)
				return
			}
			t.Logf("cleanup: restored database to version 10")
		case v == 10:
			t.Logf("cleanup: database already at version 10")
		default:
			t.Errorf("cleanup: unexpected version %d dirty=%v (only 9->10 handled). Manual recovery: SELECT * FROM schema_migrations; inspect tables; fix only after the schema matches", v, d)
		}
	})

	// 1. Snapshot at version 10.
	s10 := snapshotSchema(t, pool)
	for _, want := range []string{"table|projects\n", "table|project_versions\n", "table|project_attachments\n", "uq_project_versions_project_number"} {
		if !strings.Contains(s10, want) {
			t.Fatalf("precondition: version-10 snapshot must contain %q", want)
		}
	}

	// 2. Down to 9 (only 0010's down file ever runs here; never below 9).
	migrateTo(t, normalized, 9)
	v9, d9, err := readMigrationVersion(ctx, pool)
	if err != nil {
		t.Fatalf("read schema_migrations after down: %v", err)
	}
	if v9 != 9 || d9 {
		t.Fatalf("after down: version=%d dirty=%v, want version=9 dirty=false", v9, d9)
	}
	for _, tbl := range roundtripTables0010 {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname='public' AND tablename=$1)`, tbl).Scan(&exists); err != nil {
			t.Fatalf("check table %s after down: %v", tbl, err)
		}
		if exists {
			t.Fatalf("table %s still exists after 0010 down", tbl)
		}
	}
	var uqExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='uq_project_versions_project_number')`).Scan(&uqExists); err != nil {
		t.Fatalf("check uq constraint after down: %v", err)
	}
	if uqExists {
		t.Fatalf("constraint uq_project_versions_project_number still exists after 0010 down")
	}
	s9 := snapshotSchema(t, pool)

	// 3. v9 snapshot must equal v10 snapshot minus all 0010 objects:
	// tables 0001-0009 are byte-for-byte untouched.
	if filtered := without0010Objects(s10); filtered != s9 {
		t.Fatalf("0001-0009 objects changed by the 0010 round-trip:\n%s", diffSnapshots(filtered, s9))
	}
	t.Logf("0001-0009 objects untouched by 0010 down (%d schema lines)", len(sortedLines(s9)))

	// 4. Back up to 10; must match the original v10 snapshot exactly.
	migrateTo(t, normalized, 10)
	v10, d10, err := readMigrationVersion(ctx, pool)
	if err != nil {
		t.Fatalf("read schema_migrations after re-up: %v", err)
	}
	if v10 != 10 || d10 {
		t.Fatalf("after re-up: version=%d dirty=%v, want version=10 dirty=false", v10, d10)
	}
	if s10b := snapshotSchema(t, pool); s10b != s10 {
		t.Fatalf("schema after 0010 re-up differs from original v10 snapshot:\n%s", diffSnapshots(s10, s10b))
	}
	t.Logf("round-trip 10->9->10 stable: %d schema lines", len(sortedLines(s10)))

	// 5. Short functional check on the restored schema, using the real
	// column list from 0001/0010 (id, email, password_hash / user_id, title /
	// project_id, version_number, kind, prompt, html).
	email := fmt.Sprintf("roundtrip-%d@example.com", time.Now().UnixNano())
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(), $1, 'dummy') RETURNING id`,
		email).Scan(&userID); err != nil {
		t.Fatalf("insert temp user: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, title) VALUES ($1, 'Roundtrip') RETURNING id`,
		userID).Scan(&projectID); err != nil {
		t.Fatalf("insert temp project: %v", err)
	}
	for _, n := range []int{1, 2} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO project_versions (project_id, version_number, kind, prompt, html) VALUES ($1, $2, 'generate', '', '<p>x</p>')`,
			projectID, n); err != nil {
			t.Fatalf("insert temp version %d: %v", n, err)
		}
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO project_versions (project_id, version_number, kind, prompt, html) VALUES ($1, 1, 'generate', '', '<p>dup</p>')`,
		projectID)
	if err == nil {
		t.Fatalf("duplicate version_number 1 was accepted; uq_project_versions_project_number is not enforced")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("expected unique violation 23505 for duplicate version, got: %v", err)
	}
	t.Logf("unique constraint uq_project_versions_project_number enforced (23505 on duplicate)")
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete temp user (cascade): %v", err)
	}
	t.Logf("temp user/project/versions removed via cascade")
}
