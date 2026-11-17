package aistate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var testWIB = time.FixedZone("WIB", 7*60*60)

// fakeStoreDB mimics the SQL semantics used by Store: an upsert that increments
// generate_count, an upsert that keeps the GREATEST exhausted_until, a
// window-scoped SELECT, and an exact-brief cache upsert/select.
type fakeStoreDB struct {
	quota map[string]fakeQuotaRow
	cache map[string]string
}

type fakeQuotaRow struct {
	model          string
	date           string
	generateCount  int
	exhaustedUntil *time.Time
}

func newFakeStoreDB() *fakeStoreDB {
	return &fakeStoreDB{
		quota: make(map[string]fakeQuotaRow),
		cache: make(map[string]string),
	}
}

func quotaKey(model, date string) string {
	return model + "\x00" + date
}

func (f *fakeStoreDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := ctx.Err(); err != nil {
		return pgconn.CommandTag{}, err
	}

	switch {
	case strings.Contains(sql, "INSERT INTO ai_quota_usage") &&
		strings.Contains(sql, "generate_count = ai_quota_usage.generate_count + 1"):
		model, date, err := quotaArgs(args)
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		key := quotaKey(model, date)
		row := f.quota[key]
		row.model, row.date = model, date
		row.generateCount++
		f.quota[key] = row
		return pgconn.NewCommandTag("INSERT 0 1"), nil

	case strings.Contains(sql, "INSERT INTO ai_quota_usage") &&
		strings.Contains(sql, "exhausted_until = GREATEST"):
		model, date, err := quotaArgs(args)
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		if len(args) != 3 {
			return pgconn.CommandTag{}, fmt.Errorf("mark exhausted args = %d, want 3", len(args))
		}
		until, ok := args[2].(time.Time)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("until type = %T, want time.Time", args[2])
		}
		key := quotaKey(model, date)
		row := f.quota[key]
		row.model, row.date = model, date
		if row.exhaustedUntil == nil || until.After(*row.exhaustedUntil) {
			value := until
			row.exhaustedUntil = &value
		}
		f.quota[key] = row
		return pgconn.NewCommandTag("INSERT 0 1"), nil

	case strings.Contains(sql, "INSERT INTO ai_html_cache"):
		if len(args) != 2 {
			return pgconn.CommandTag{}, fmt.Errorf("put html args = %d, want 2", len(args))
		}
		hash, ok := args[0].(string)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("hash type = %T, want string", args[0])
		}
		html, ok := args[1].(string)
		if !ok {
			return pgconn.CommandTag{}, fmt.Errorf("html type = %T, want string", args[1])
		}
		f.cache[hash] = html
		return pgconn.NewCommandTag("INSERT 0 1"), nil

	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected Exec SQL: %s", sql)
	}
}

func quotaArgs(args []any) (model, date string, err error) {
	if len(args) < 2 {
		return "", "", fmt.Errorf("quota args = %d, want at least 2", len(args))
	}
	model, ok := args[0].(string)
	if !ok {
		return "", "", fmt.Errorf("model type = %T, want string", args[0])
	}
	date, ok = args[1].(string)
	if !ok {
		return "", "", fmt.Errorf("window date type = %T, want string", args[1])
	}
	return model, date, nil
}

func (f *fakeStoreDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(sql, "FROM ai_quota_usage") || !strings.Contains(sql, "window_date") {
		return nil, fmt.Errorf("unexpected Query SQL: %s", sql)
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("load quota args = %d, want 1", len(args))
	}
	date, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("window date type = %T, want string", args[0])
	}

	rows := make([]fakeQuotaRow, 0, len(f.quota))
	for _, row := range f.quota {
		if row.date == date {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].model < rows[j].model })
	return &fakeRows{rows: rows}, nil
}

func (f *fakeStoreDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := ctx.Err(); err != nil {
		return fakeRow{err: err}
	}
	if !strings.Contains(sql, "SELECT html FROM ai_html_cache") {
		return fakeRow{err: fmt.Errorf("unexpected QueryRow SQL: %s", sql)}
	}
	if len(args) != 1 {
		return fakeRow{err: fmt.Errorf("get html args = %d, want 1", len(args))}
	}
	hash, ok := args[0].(string)
	if !ok {
		return fakeRow{err: fmt.Errorf("hash type = %T, want string", args[0])}
	}
	html, ok := f.cache[hash]
	if !ok {
		return fakeRow{err: pgx.ErrNoRows}
	}
	return fakeRow{html: html}
}

type fakeRow struct {
	html string
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("scan destinations = %d, want 1", len(dest))
	}
	target, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("scan destination type = %T, want *string", dest[0])
	}
	*target = r.html
	return nil
}

type fakeRows struct {
	rows   []fakeQuotaRow
	index  int
	closed bool
}

func (r *fakeRows) Close() { r.closed = true }

func (r *fakeRows) Err() error { return nil }

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.NewCommandTag("SELECT 0")
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.index == 0 || r.index > len(r.rows) {
		return fmt.Errorf("Scan called without Next")
	}
	if len(dest) != 3 {
		return fmt.Errorf("scan destinations = %d, want 3", len(dest))
	}
	row := r.rows[r.index-1]

	model, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("model destination type = %T, want *string", dest[0])
	}
	count, ok := dest[1].(*int)
	if !ok {
		return fmt.Errorf("count destination type = %T, want *int", dest[1])
	}
	exhausted, ok := dest[2].(**time.Time)
	if !ok {
		return fmt.Errorf("exhausted destination type = %T, want **time.Time", dest[2])
	}

	*model = row.model
	*count = row.generateCount
	if row.exhaustedUntil == nil {
		*exhausted = nil
	} else {
		value := *row.exhaustedUntil
		*exhausted = &value
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error) {
	if r.index == 0 || r.index > len(r.rows) {
		return nil, fmt.Errorf("Values called without Next")
	}
	row := r.rows[r.index-1]
	return []any{row.model, row.generateCount, row.exhaustedUntil}, nil
}

func (r *fakeRows) RawValues() [][]byte { return nil }

func (r *fakeRows) Conn() *pgx.Conn { return nil }

func TestStoreIncrementGeneratePersistsAcrossStoreInstances(t *testing.T) {
	db := newFakeStoreDB()
	ctx := context.Background()
	window := time.Date(2026, 11, 17, 15, 0, 0, 0, testWIB)

	first := &Store{db: db}
	for i := 0; i < 3; i++ {
		if err := first.IncrementGenerate(ctx, "gemini-3.8-flash", window); err != nil {
			t.Fatalf("IncrementGenerate(%d): %v", i, err)
		}
	}

	// A new Store over the same database must see the persisted counter.
	second := &Store{db: db}
	records, err := second.LoadQuota(ctx, window)
	if err != nil {
		t.Fatalf("LoadQuota: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if got := records[0]; got.Model != "gemini-3.8-flash" || got.GenerateCount != 3 {
		t.Errorf("record = %+v, want gemini-3.8-flash count 3", got)
	}
}

func TestStoreQuotaWindowResetsAt1500WIB(t *testing.T) {
	db := newFakeStoreDB()
	ctx := context.Background()
	previous := time.Date(2026, 11, 16, 15, 0, 0, 0, testWIB)
	current := time.Date(2026, 11, 17, 15, 0, 0, 0, testWIB)

	if got := windowDate(previous); got != "2026-11-16" {
		t.Fatalf("previous windowDate = %q, want 2026-11-16", got)
	}
	if got := windowDate(current); got != "2026-11-17" {
		t.Fatalf("current windowDate = %q, want 2026-11-17", got)
	}

	store := &Store{db: db}
	if err := store.IncrementGenerate(ctx, "gemini-3.8-flash", previous); err != nil {
		t.Fatalf("increment previous window: %v", err)
	}
	if err := store.IncrementGenerate(ctx, "gemini-3.8-flash", current); err != nil {
		t.Fatalf("increment current window: %v", err)
	}

	// The 15:00 WIB reset is a new window_date, so the old row is ignored.
	currentRecords, err := store.LoadQuota(ctx, current)
	if err != nil {
		t.Fatalf("LoadQuota current: %v", err)
	}
	if len(currentRecords) != 1 || currentRecords[0].GenerateCount != 1 {
		t.Errorf("current records = %+v, want one row with count 1", currentRecords)
	}

	previousRecords, err := store.LoadQuota(ctx, previous)
	if err != nil {
		t.Fatalf("LoadQuota previous: %v", err)
	}
	if len(previousRecords) != 1 || previousRecords[0].GenerateCount != 1 {
		t.Errorf("previous records = %+v, want one row with count 1", previousRecords)
	}
}

func TestStoreMarkExhaustedKeepsLatestDeadline(t *testing.T) {
	db := newFakeStoreDB()
	ctx := context.Background()
	window := time.Date(2026, 11, 17, 15, 0, 0, 0, testWIB)
	early := window.Add(2 * time.Hour)
	late := window.Add(4 * time.Hour)

	store := &Store{db: db}
	if err := store.MarkExhausted(ctx, "gemini-3.8-flash", window, late); err != nil {
		t.Fatalf("MarkExhausted late: %v", err)
	}
	if err := store.MarkExhausted(ctx, "gemini-3.8-flash", window, early); err != nil {
		t.Fatalf("MarkExhausted early: %v", err)
	}

	records, err := store.LoadQuota(ctx, window)
	if err != nil {
		t.Fatalf("LoadQuota: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if got := records[0].ExhaustedUntil; !got.Equal(late) {
		t.Errorf("ExhaustedUntil = %v, want GREATEST %v", got, late)
	}

	recreated := &Store{db: db}
	records, err = recreated.LoadQuota(ctx, window)
	if err != nil {
		t.Fatalf("LoadQuota after recreate: %v", err)
	}
	if len(records) != 1 || !records[0].ExhaustedUntil.Equal(late) {
		t.Errorf("exhausted marker did not survive Store recreation: %+v", records)
	}
}

func TestStoreHTMLCachePutGetAndPersistsAcrossStoreInstances(t *testing.T) {
	db := newFakeStoreDB()
	ctx := context.Background()
	hash := strings.Repeat("a", 64)

	first := &Store{db: db}
	if err := first.PutHTML(ctx, hash, "<html>first</html>"); err != nil {
		t.Fatalf("PutHTML: %v", err)
	}
	if err := first.PutHTML(ctx, hash, "<html>updated</html>"); err != nil {
		t.Fatalf("PutHTML update: %v", err)
	}

	second := &Store{db: db}
	html, ok, err := second.GetHTML(ctx, hash)
	if err != nil {
		t.Fatalf("GetHTML: %v", err)
	}
	if !ok {
		t.Fatal("GetHTML ok = false, want true")
	}
	if html != "<html>updated</html>" {
		t.Errorf("GetHTML = %q, want updated value", html)
	}

	if _, ok, err := second.GetHTML(ctx, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("GetHTML missing: %v", err)
	} else if ok {
		t.Error("GetHTML missing ok = true, want false")
	}
}
