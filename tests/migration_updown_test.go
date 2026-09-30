// Migration round-trip test: 0001-0010 up, down, up.
//
// Alur: migrasikan ke versi 9 → snapshot skema; ke versi 10 → snapshot;
// turun ke 9 → snapshot harus SAMA dengan snapshot versi 9; naik ke 10 →
// snapshot harus SAMA dengan snapshot versi 10 pertama.
//
// Berjalan HANYA jika TEST_DATABASE_URL menunjuk ke database Postgres kosong
// khusus test (jangan pernah produksi). Tanpa variabel itu test di-skip.
// golang-migrate tidak mendukung context, jadi operasi migrasinya sendiri
// tidak bisa dibatasi timeout; semua query snapshot/cleanup memakai context
// 2 menit dan koneksi memakai connect timeout 15 detik.
package tests

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/database"
)

// testConnTimeout bounds every DB operation in this file (2 menit per test).
const testConnTimeout = 2 * time.Minute

// requireMigrationTestDB validates the TEST_DATABASE_URL safety guards and
// returns an open pool. It performs NO schema change.
func requireMigrationTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a blank Postgres test database to run migration round-trip tests")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL is not a valid URL: %v", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		t.Fatalf("TEST_DATABASE_URL must use a postgres/postgresql scheme, got %q", u.Scheme)
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	if port == "6543" {
		t.Fatalf("refusing to run: port 6543 is the Supabase transaction pooler; use the Session pooler (port 5432)")
	}
	sslmode := strings.ToLower(u.Query().Get("sslmode"))
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
	// Identitas koneksi tanpa password, untuk log dan perbandingan.
	t.Logf("test database identity: host=%s port=%s dbname=%s user=%s sslmode=%s", u.Hostname(), port, dbname, user, sslmode)

	assertNotProduction(t, u.Hostname(), port, dbname, user)

	cfg := config.DBConfig{URL: dsn, MaxConns: 4, ConnectTimeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), testConnTimeout)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(database.DSN(cfg))
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	poolCfg.ConnConfig.ConnectTimeout = 15 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	// Pengaman 1: schema public harus kosong (tidak ada tabel apa pun,
	// termasuk sisa migrasi sebelumnya) sebelum test dimulai.
	var tables []string
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY 1`)
	if err != nil {
		t.Fatalf("list public tables: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan public tables: %v", err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("list public tables: %v", err)
	}
	if len(tables) > 0 {
		t.Fatalf("refusing to run: schema public is not empty: %v", tables)
	}
	return pool
}

// assertNotProduction refuses to run when TEST_DATABASE_URL identifies the
// same database as the production DATABASE_URL from the repo-root .env.
// Identitas = host + port + nama database + USER (user ikut dibandingkan
// karena pooler Supabase dipakai bersama; yang membedakan project adalah
// user postgres.<project-ref>). Password tidak pernah dibaca untuk
// perbandingan dan tidak pernah dicetak.
func assertNotProduction(t *testing.T, host, port, dbname, user string) {
	t.Helper()
	prodDSN := productionDatabaseURL(t)
	if prodDSN == "" {
		t.Logf("WARNING: production DATABASE_URL not found in repo-root .env; identity comparison skipped, proceeding on your responsibility")
		return
	}
	pu, err := url.Parse(prodDSN)
	if err != nil {
		t.Logf("WARNING: production DATABASE_URL does not parse (%v); identity comparison skipped, proceeding on your responsibility", err)
		return
	}
	pport := pu.Port()
	if pport == "" {
		pport = "5432"
	}
	puser := ""
	if pu.User != nil {
		puser = pu.User.Username()
	}
	pdb := strings.TrimPrefix(pu.Path, "/")
	if strings.EqualFold(pu.Hostname(), host) && pport == port && pdb == dbname && puser == user {
		t.Fatalf("refusing to run: TEST_DATABASE_URL identifies the same database as production DATABASE_URL (host+port+dbname+user match)")
	}
	t.Logf("production identity differs from test identity; proceeding")
}

// productionDatabaseURL reads only the DATABASE_URL line from the repo-root
// .env (resolved relative to this file, not the process CWD). Callers use it
// solely for identity comparison; the value is never logged.
func productionDatabaseURL(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	envPath := filepath.Join(filepath.Dir(thisFile), "..", ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "DATABASE_URL" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		return value
	}
	return ""
}

// snapshotSchema serializes tables, columns, indexes, and constraints in
// schema public (minus the migrate bookkeeping table) into a canonical
// string for equality comparison.
func snapshotSchema(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnTimeout)
	defer cancel()

	var b strings.Builder
	query := func(name, q string) {
		rows, err := pool.Query(ctx, q)
		if err != nil {
			t.Fatalf("snapshot %s: %v", name, err)
		}
		defer rows.Close()
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				t.Fatalf("snapshot %s: %v", name, err)
			}
			b.WriteString(name + "|")
			for i, v := range vals {
				if i > 0 {
					b.WriteString("|")
				}
				b.WriteString(fmt.Sprintf("%v", v))
			}
			b.WriteString("\n")
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("snapshot %s: %v", name, err)
		}
	}

	query("table", `SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'schema_migrations' ORDER BY 1`)
	query("column", `SELECT table_name, column_name, data_type, udt_name, is_nullable,
		coalesce(column_default, '') FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name <> 'schema_migrations' ORDER BY 1, 2`)
	query("index", `SELECT indexname, indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND tablename <> 'schema_migrations' ORDER BY 1`)
	query("constraint", `SELECT t.relname, c.conname, c.contype, pg_get_constraintdef(c.oid)
		FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = 'public' AND t.relname <> 'schema_migrations' AND t.relkind = 'r'
		ORDER BY 1, 2`)
	return b.String()
}

func sortedLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	sort.Strings(lines)
	return lines
}

func diffSnapshots(a, b string) string {
	la, lb := sortedLines(a), sortedLines(b)
	setB := make(map[string]int)
	for _, l := range lb {
		setB[l]++
	}
	var missing []string
	for _, l := range la {
		if setB[l] > 0 {
			setB[l]--
		} else {
			missing = append(missing, l)
		}
	}
	setA := make(map[string]int)
	for _, l := range la {
		setA[l]++
	}
	var extra []string
	for _, l := range lb {
		if setA[l] > 0 {
			setA[l]--
		} else {
			extra = append(extra, l)
		}
	}
	out := ""
	if len(missing) > 0 {
		out += fmt.Sprintf("missing (%d):\n  %s\n", len(missing), strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		out += fmt.Sprintf("extra (%d):\n  %s\n", len(extra), strings.Join(extra, "\n  "))
	}
	return out
}

// cleanupMigrationObjects drops only the objects owned by this repo's
// migrations (children first, CASCADE each) plus the migrate bookkeeping
// table. It never touches schema public itself: on Supabase, dropping the
// schema can destroy its default privileges.
func cleanupMigrationObjects(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnTimeout)
	defer cancel()
	ordered := []string{
		"project_attachments", "project_versions",
		"message_attachments", "messages", "conversations",
		"document_chunks", "documents",
		"code_executions", "projects", "users",
		"schema_migrations",
	}
	for _, tbl := range ordered {
		if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS `+tbl+` CASCADE`); err != nil {
			t.Fatalf("cleanup drop %s: %v", tbl, err)
		}
	}
	var remaining []string
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY 1`)
	if err != nil {
		t.Fatalf("verify cleanup: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("verify cleanup: %v", err)
		}
		remaining = append(remaining, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("verify cleanup: %v", err)
	}
	if len(remaining) > 0 {
		t.Fatalf("cleanup incomplete, public still holds: %v", remaining)
	}
}

func migrateTo(t *testing.T, dsn string, version uint) {
	t.Helper()
	if err := database.MigrateTo(config.DBConfig{URL: dsn}, version); err != nil {
		t.Fatalf("MigrateTo(%d): %v", version, err)
	}
}

func TestMigrationIntegration_UpDownUp0010(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	pool := requireMigrationTestDB(t)
	t.Cleanup(func() { cleanupMigrationObjects(t, pool) })

	migrateTo(t, dsn, 9)
	snap9 := snapshotSchema(t, pool)
	if strings.Contains(snap9, "table|projects\n") {
		t.Fatalf("snapshot after v9 must not contain projects table")
	}

	migrateTo(t, dsn, 10)
	snap10a := snapshotSchema(t, pool)
	for _, want := range []string{"table|projects\n", "table|project_versions\n", "table|project_attachments\n", "uq_project_versions_project_number"} {
		if !strings.Contains(snap10a, want) {
			t.Fatalf("snapshot after v10-up must contain %q", want)
		}
	}

	migrateTo(t, dsn, 9)
	snap9b := snapshotSchema(t, pool)
	if snap9b != snap9 {
		t.Fatalf("schema after 0010-down differs from after-0009 snapshot:\n%s", diffSnapshots(snap9, snap9b))
	}

	migrateTo(t, dsn, 10)
	snap10b := snapshotSchema(t, pool)
	if snap10b != snap10a {
		t.Fatalf("schema after 0010-up-again differs from first 0010-up snapshot:\n%s", diffSnapshots(snap10a, snap10b))
	}
	t.Logf("round-trip 9→10→9→10 stable: %d schema lines", len(sortedLines(snap10b)))
}
