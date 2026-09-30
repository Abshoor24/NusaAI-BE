// Package tests holds integration tests that require real infrastructure.
//
// Project repository integration tests (Postgres asli).
//
// Cara menjalankan (database HARUS kosong dan BUKAN produksi):
//
//	docker run -d --name be-test-pg \
//	  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=backend_test \
//	  -p 5433:5432 pgvector/pgvector:pg17
//	TEST_DATABASE_URL=postgresql://postgres:postgres@localhost:5433/backend_test?sslmode=disable \
//	  go test ./tests/ -run TestProjectIntegration -v -count=1
//
// Image pgvector dibutuhkan karena migrasi 0006 memakai ekstensi `vector`.
// Tanpa TEST_DATABASE_URL semua test di file ini di-skip sehingga
// `go test ./...` biasa tetap hijau.
package tests

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/database"
	projectmodel "github.com/Abshoor24/backend-go/internal/modules/project/model"
	projectrepo "github.com/Abshoor24/backend-go/internal/modules/project/repository"
)

// requireProjectTestDB connects to the TEST_DATABASE_URL database, runs all
// embedded migrations (currently through 0011) on it, and returns the pool
// plus a project repository. It never touches the app .env / DATABASE_URL: TEST_DATABASE_URL
// is read directly so a production connection string in .env cannot leak in.
func requireProjectTestDB(t *testing.T) (*pgxpool.Pool, projectrepo.Repository) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a blank Postgres database to run project integration tests")
	}

	dbCfg := config.DBConfig{
		URL: dsn,
		// Dibatasi 8 koneksi agar tidak melampaui batas Session pooler Supabase
		// (free tier). Ini tidak mengurangi bukti perbaikan race penomoran:
		// serialisasi datang dari SELECT ... FOR UPDATE di FinishSuccess,
		// bukan dari jumlah koneksi — goroutine berlebih antre di pool.
		MaxConns:          8,
		MinConns:          0,
		MaxConnLifetime:   time.Hour,
		ConnectTimeout:    15 * time.Second,
		HealthCheckPeriod: time.Minute,
	}

	if err := database.RunMigrations(dbCfg); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := database.New(ctx, dbCfg)
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(db.Close)

	return db.Pool(), projectrepo.New(db.Pool())
}

// createTestUser inserts a throwaway user row and deletes it (cascading to
// its projects, versions, and attachments) when the test finishes.
func createTestUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	email := fmt.Sprintf("integ-%s@example.com", id)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'test-hash-not-for-login')`,
		id, email); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup test user: %v", err)
		}
	})
	return id
}

func createTestProject(t *testing.T, repo projectrepo.Repository, userID string) *projectmodel.Project {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := repo.Create(ctx, userID, "Integration Project", projectmodel.Brief{
		Description: "Project integrasi untuk menguji atomisitas repository.",
		WebsiteType: "landing_page",
		Language:    "id",
	})
	if err != nil {
		t.Fatalf("repo.Create() error = %v", err)
	}
	if p.Status != projectmodel.StatusDraft {
		t.Fatalf("new project status = %q, want draft", p.Status)
	}
	return p
}

func appCode(t *testing.T, err error) string {
	t.Helper()
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apperror.Error, got %T (%v)", err, err)
	}
	return ae.Code()
}

// a. Tepat satu goroutine yang boleh memenangkan transisi ke generating;
// sisanya mendapat CONFLICT ("sudah generating", yang di HTTP menjadi 409).
func TestProjectIntegration_UsersTableHasNoRoleColumn(t *testing.T) {
	pool, _ := requireProjectTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'role'
	)`).Scan(&exists); err != nil {
		t.Fatalf("check role column: %v", err)
	}
	if exists {
		t.Error("users.role column still exists; migration 0011 must have dropped it")
	}
}

func TestProjectIntegration_AtomicTryStart(t *testing.T) {
	pool, repo := requireProjectTestDB(t)
	userID := createTestUser(t, pool)
	p := createTestProject(t, repo, userID)

	const n = 10
	var wg sync.WaitGroup
	wins := make(chan *projectmodel.Project, n)
	errs := make(chan error, n)
	// Timeout 2 menit untuk seluruh percobaan paralel (cloud latency).
	opCtx, opCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer opCancel()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := repo.TryStartGenerate(opCtx, userID, p.ID)
			if err != nil {
				errs <- err
				return
			}
			wins <- got
		}()
	}
	wg.Wait()
	close(wins)
	close(errs)

	var winners []*projectmodel.Project
	for w := range wins {
		winners = append(winners, w)
	}
	var losers []error
	for e := range errs {
		losers = append(losers, e)
	}
	if len(winners) != 1 {
		t.Fatalf("winners = %d, want exactly 1", len(winners))
	}
	if winners[0].Status != projectmodel.StatusGenerating {
		t.Errorf("winner status = %q, want generating", winners[0].Status)
	}
	if len(losers) != n-1 {
		t.Fatalf("losers = %d, want %d", len(losers), n-1)
	}
	for _, err := range losers {
		if code := appCode(t, err); code != "CONFLICT" {
			t.Errorf("loser code = %q, want CONFLICT", code)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, found, err := repo.LookupStatus(ctx, userID, p.ID)
	if err != nil || !found || status != projectmodel.StatusGenerating {
		t.Errorf("LookupStatus = %q/%v/%v, want generating/true/nil", status, found, err)
	}
}

// b. N FinishSuccess paralel harus menghasilkan nomor versi 1..1+N yang unik
// dan berurutan tanpa celah/duplikat. Ini pembuktian perbaikan race
// penomoran versi: FinishSuccess mengunci baris project (SELECT ... FOR
// UPDATE) sebelum menghitung MAX+1, sehingga finish konkuren antre pada
// lock dan tidak bertabrakan pada UNIQUE (project_id, version_number).
func TestProjectIntegration_ConcurrentVersionNumbering(t *testing.T) {
	pool, repo := requireProjectTestDB(t)
	userID := createTestUser(t, pool)
	p := createTestProject(t, repo, userID)

	// Unique constraint pengaman terakhir — laporkan keberadaannya.
	var constraintName string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = pool.QueryRow(ctx, `SELECT conname FROM pg_constraint
		WHERE conrelid = 'project_versions'::regclass AND contype = 'u'`).Scan(&constraintName)
	t.Logf("unique constraint on project_versions: %q", constraintName)

	if _, err := repo.TryStartGenerate(ctx, userID, p.ID); err != nil {
		t.Fatalf("TryStartGenerate() error = %v", err)
	}
	if _, err := repo.FinishSuccess(ctx, p.ID, projectmodel.KindGenerate, "brief", "<!DOCTYPE html><html></html>"); err != nil {
		t.Fatalf("seed FinishSuccess() error = %v", err)
	}

	const n = 15
	var wg sync.WaitGroup
	errs := make(chan error, n)
	// Timeout 2 menit untuk seluruh percobaan paralel (cloud latency).
	opCtx, opCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer opCancel()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.FinishSuccess(opCtx, p.ID,
				projectmodel.KindRevise, fmt.Sprintf("revise %d", i), "<!DOCTYPE html><html></html>")
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	for _, err := range failures {
		t.Logf("parallel FinishSuccess error: %v", err)
	}
	if len(failures) > 0 {
		t.Errorf("%d of %d parallel FinishSuccess calls failed (see log) — version numbering is not safe under concurrency", len(failures), n)
	}

	versions, err := repo.ListVersions(ctx, userID, p.ID)
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	numbers := make([]int, 0, len(versions))
	for _, v := range versions {
		numbers = append(numbers, v.VersionNumber)
	}
	sort.Ints(numbers)
	wantTotal := 1 + n - len(failures)
	if len(numbers) != wantTotal {
		t.Fatalf("stored versions = %d, want %d", len(numbers), wantTotal)
	}
	for i, num := range numbers {
		if num != i+1 {
			t.Fatalf("version numbers = %v, want gapless 1..%d", numbers, wantTotal)
		}
	}
}

// c. Status kembali normal setelah generate gagal / context dibatalkan.
func TestProjectIntegration_StatusRecovery(t *testing.T) {
	pool, repo := requireProjectTestDB(t)
	userID := createTestUser(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Generate gagal -> failed.
	p1 := createTestProject(t, repo, userID)
	if _, err := repo.TryStartGenerate(ctx, userID, p1.ID); err != nil {
		t.Fatalf("TryStartGenerate() error = %v", err)
	}
	if err := repo.FinishFailedGenerate(ctx, p1.ID); err != nil {
		t.Fatalf("FinishFailedGenerate() error = %v", err)
	}
	if status, _, _ := repo.LookupStatus(ctx, userID, p1.ID); status != projectmodel.StatusFailed {
		t.Errorf("after failed generate status = %q, want failed", status)
	}

	// Revise gagal -> ready, versi lama utuh.
	p2 := createTestProject(t, repo, userID)
	if _, err := repo.TryStartGenerate(ctx, userID, p2.ID); err != nil {
		t.Fatalf("TryStartGenerate() error = %v", err)
	}
	if _, err := repo.FinishSuccess(ctx, p2.ID, projectmodel.KindGenerate, "brief", "<!DOCTYPE html><html></html>"); err != nil {
		t.Fatalf("FinishSuccess() error = %v", err)
	}
	if _, err := repo.TryStartRevise(ctx, userID, p2.ID); err != nil {
		t.Fatalf("TryStartRevise() error = %v", err)
	}
	if err := repo.FinishFailedRevise(ctx, p2.ID); err != nil {
		t.Fatalf("FinishFailedRevise() error = %v", err)
	}
	if status, _, _ := repo.LookupStatus(ctx, userID, p2.ID); status != projectmodel.StatusReady {
		t.Errorf("after failed revise status = %q, want ready", status)
	}
	got, _, err := repo.FindWithLatest(ctx, userID, p2.ID)
	if err != nil || got.VersionCount != 1 {
		t.Errorf("after failed revise version_count = %d, %v; want 1", got.VersionCount, err)
	}

	// Context dibatalkan di tengah jalan -> error, tidak ada versi tersimpan.
	p3 := createTestProject(t, repo, userID)
	if _, err := repo.TryStartGenerate(ctx, userID, p3.ID); err != nil {
		t.Fatalf("TryStartGenerate() error = %v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := repo.FinishSuccess(cancelled, p3.ID, projectmodel.KindGenerate, "brief", "<!DOCTYPE html><html></html>"); err == nil {
		t.Error("FinishSuccess with cancelled context should fail")
	} else {
		t.Logf("cancelled FinishSuccess error (expected): %v", err)
	}
	versions, err := repo.ListVersions(ctx, userID, p3.ID)
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("stored versions after cancelled finish = %d, want 0", len(versions))
	}
}
