package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/aistate"
)

type fakeQuotaStore struct {
	mu         sync.Mutex
	records    map[string]aistate.QuotaRecord
	increments map[string]int
	exhausted  map[string]time.Time
}

func newFakeQuotaStore() *fakeQuotaStore {
	return &fakeQuotaStore{
		records:    make(map[string]aistate.QuotaRecord),
		increments: make(map[string]int),
		exhausted:  make(map[string]time.Time),
	}
}

func (f *fakeQuotaStore) LoadQuota(_ context.Context, _ time.Time) ([]aistate.QuotaRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]aistate.QuotaRecord, 0, len(f.records))
	for _, rec := range f.records {
		out = append(out, rec)
	}
	return out, nil
}

func (f *fakeQuotaStore) IncrementGenerate(_ context.Context, model string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := f.records[model]
	rec.Model = model
	rec.GenerateCount++
	f.records[model] = rec
	f.increments[model]++
	return nil
}

func (f *fakeQuotaStore) MarkExhausted(_ context.Context, model string, _ time.Time, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec := f.records[model]
	rec.Model = model
	rec.ExhaustedUntil = until
	f.records[model] = rec
	f.exhausted[model] = until
	return nil
}

type fakeCacheStore struct {
	mu    sync.Mutex
	html  map[string]string
	gets  int
	puts  int
	lastH string
}

func newFakeCacheStore() *fakeCacheStore {
	return &fakeCacheStore{html: make(map[string]string)}
}

func (f *fakeCacheStore) GetHTML(_ context.Context, hash string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	f.lastH = hash
	html, ok := f.html[hash]
	return html, ok, nil
}

func (f *fakeCacheStore) PutHTML(_ context.Context, hash, html string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	f.html[hash] = html
	return nil
}

func TestQuotaWindowResetsAt1500WIB(t *testing.T) {
	wib := time.FixedZone("WIB", 7*60*60)
	before := time.Date(2026, 11, 17, 14, 59, 0, 0, wib)
	after := time.Date(2026, 11, 17, 15, 0, 0, 0, wib)
	wantBefore := time.Date(2026, 11, 16, 15, 0, 0, 0, wib)
	wantAfter := after

	if got := quotaWindowStart(before); !got.Equal(wantBefore) {
		t.Errorf("window before reset = %v, want %v", got, wantBefore)
	}
	if got := quotaWindowStart(after); !got.Equal(wantAfter) {
		t.Errorf("window at reset = %v, want %v", got, wantAfter)
	}
	if got := quotaResetAt(wantBefore); !got.Equal(after) {
		t.Errorf("reset = %v, want %v", got, after)
	}
}

func TestQuotaStatusReportsPersistedUsagePerModel(t *testing.T) {
	store := newFakeQuotaStore()
	store.records["primary-model"] = aistate.QuotaRecord{Model: "primary-model", GenerateCount: 19}
	store.records["fallback-model"] = aistate.QuotaRecord{Model: "fallback-model", GenerateCount: 5}
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	aiCfg.DailyQuotaLimit = 20
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), newFakeProvider(testHTML), aiCfg, uploadCfg, projectCfg,
		testLogger(), WithQuotaStore(store))
	impl := svc.(*service)

	status := impl.QuotaStatus(context.Background())
	if status.DailyLimit != 20 {
		t.Fatalf("DailyLimit = %d, want 20", status.DailyLimit)
	}
	if len(status.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(status.Models))
	}
	primary, fallback := status.Models[0], status.Models[1]
	if primary.Model != "primary-model" || primary.Used != 19 || primary.Remaining != 1 || primary.Exhausted {
		t.Errorf("primary status = %+v, want used 19 remaining 1 available", primary)
	}
	if fallback.Model != "fallback-model" || fallback.Used != 5 || fallback.Remaining != 15 {
		t.Errorf("fallback status = %+v, want used 5 remaining 15", fallback)
	}
	if !status.WindowStart.Equal(quotaWindowStart(time.Now()).In(wib)) {
		t.Errorf("WindowStart = %v, want current 15:00 WIB window", status.WindowStart)
	}
}

func TestGenerateCachedBriefSkipsProvider(t *testing.T) {
	fp := newFakeProvider(testHTML)
	cache := newFakeCacheStore()
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.DailyQuotaLimit = 20
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger(), WithCacheStore(cache))
	ctx := context.Background()

	first := createDraft(t, svc, "user-1")
	firstPending, err := svc.BeginGenerate(ctx, "user-1", first.ID, nil)
	if err != nil {
		t.Fatalf("first BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, firstPending); ae != nil {
		t.Fatalf("first finish: %v", ae)
	}
	if cache.puts != 1 {
		t.Fatalf("cache puts = %d, want 1", cache.puts)
	}

	second := createDraft(t, svc, "user-1")
	secondPending, err := svc.BeginGenerate(ctx, "user-1", second.ID, nil)
	if err != nil {
		t.Fatalf("second BeginGenerate: %v", err)
	}
	if !secondPending.CacheHit {
		t.Fatal("second pending CacheHit = false, want true")
	}
	if secondPending.Model != "cache" {
		t.Errorf("second model = %q, want cache", secondPending.Model)
	}
	if fp.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (cache hit must not call the model)", fp.calls)
	}
	v, ae := drainAndFinish(ctx, t, svc, secondPending)
	if ae != nil {
		t.Fatalf("second finish: %v", ae)
	}
	if v.HTML != testHTML {
		t.Errorf("cached version HTML mismatch")
	}
	if cache.puts != 2 {
		t.Errorf("cache puts = %d, want 2 (cache hit revalidates and stores)", cache.puts)
	}
}

func TestQuotaUsageSurvivesServiceRestart(t *testing.T) {
	store := newFakeQuotaStore()
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.DailyQuotaLimit = 20
	uploadCfg.Dir = t.TempDir()

	firstSvc := New(newFakeRepo(), newFakeProvider(testHTML), aiCfg, uploadCfg, projectCfg,
		testLogger(), WithQuotaStore(store))
	p := createDraft(t, firstSvc, "user-1")
	pending, err := firstSvc.BeginGenerate(context.Background(), "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(context.Background(), t, firstSvc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}

	secondSvc := New(newFakeRepo(), newFakeProvider(testHTML), aiCfg, uploadCfg, projectCfg,
		testLogger(), WithQuotaStore(store)).(*service)
	status := secondSvc.QuotaStatus(context.Background())
	if len(status.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(status.Models))
	}
	if status.Models[0].Used != 1 || status.Models[0].Remaining != 19 {
		t.Errorf("after restart = %+v, want used 1 remaining 19", status.Models[0])
	}
}

func TestQuotaExhaustionPersistsAndClampsToReset(t *testing.T) {
	store := newFakeQuotaStore()
	fp := newFakeProvider(testHTML)
	fp.openErrSeq = []error{quotaOpenErr(true, "primary-model", 48*time.Hour)}
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.DailyQuotaLimit = 20
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger(), WithQuotaStore(store))
	impl := svc.(*service)
	now := time.Date(2026, 11, 17, 16, 0, 0, 0, wib)
	impl.quota.now = func() time.Time { return now }

	p := createDraft(t, svc, "user-1")
	if _, err := svc.BeginGenerate(context.Background(), "user-1", p.ID, nil); err == nil {
		t.Fatal("expected daily quota failure")
	}

	until, ok := store.exhausted["primary-model"]
	if !ok {
		t.Fatal("MarkExhausted was not persisted")
	}
	wantReset := quotaResetAt(quotaWindowStart(now))
	if !until.Equal(wantReset) {
		t.Errorf("exhausted_until = %v, want clamped to reset %v", until, wantReset)
	}
	status := impl.QuotaStatus(context.Background())
	if len(status.Models) != 1 || !status.Models[0].Exhausted || status.Models[0].Remaining != 0 {
		t.Errorf("status = %+v, want primary exhausted with remaining 0", status.Models)
	}
}

func TestQuotaLimitReachedFromCounter(t *testing.T) {
	store := newFakeQuotaStore()
	store.records["primary-model"] = aistate.QuotaRecord{Model: "primary-model", GenerateCount: 20}
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.DailyQuotaLimit = 20
	uploadCfg.Dir = t.TempDir()
	impl := New(newFakeRepo(), newFakeProvider(testHTML), aiCfg, uploadCfg, projectCfg,
		testLogger(), WithQuotaStore(store)).(*service)

	status := impl.QuotaStatus(context.Background())
	if len(status.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(status.Models))
	}
	got := status.Models[0]
	if !got.Exhausted || got.Status != "exhausted" || got.Remaining != 0 {
		t.Errorf("status = %+v, want exhausted at 20/20", got)
	}
}
