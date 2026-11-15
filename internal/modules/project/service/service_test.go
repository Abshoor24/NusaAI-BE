package service

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
	"github.com/Abshoor24/backend-go/internal/modules/project/repository"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfigs() (config.AIConfig, config.UploadConfig, config.ProjectConfig) {
	return config.AIConfig{Model: "test-model", MaxBytes: 1 << 20},
		config.UploadConfig{Dir: ""},
		config.ProjectConfig{MaxImages: 4, MaxHTMLBytes: 524288}
}

// --- fake provider ---

type fakeProvider struct {
	mu      sync.Mutex
	tokens  []string
	errAt   int // emit Err instead of token at this index (-1 = no error)
	openErr error
	// openErrSeq scripts consecutive open failures (e.g. 503 then success):
	// one entry is consumed per CompleteStream call, a nil entry succeeds.
	// When empty, openErr applies to every call.
	openErrSeq []error
	lastReq    aiprovider.Request
	calls      int
	usedImgs   int
	// doneReason is attached to the terminal Done chunk ("STOP" for a
	// normal stream). Tests set it to "" or other reasons explicitly.
	doneReason string
	doneBlock  string
	doneUsage  *aiprovider.UsageMetadata
	// openDelay simulates a slow provider open (headers never arrive in
	// time); it respects ctx cancellation.
	openDelay time.Duration
	// rejectThinking makes the fake return ErrThinkingLevelRejected whenever
	// a thinking level is sent, like a model that does not support it.
	rejectThinking bool
	// reqs records every request in call order.
	reqs []aiprovider.Request
	// ctxs records the context of every CompleteStream call.
	ctxs []context.Context
	// tokensByModel overrides tokens per model (falls back to tokens).
	tokensByModel map[string][]string
	// firstChunkDelay applies before the first token of every stream.
	firstChunkDelay time.Duration
	// firstChunkDelayByModel overrides firstChunkDelay per model.
	firstChunkDelayByModel map[string]time.Duration
}

func (f *fakeProvider) contextAt(i int) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.ctxs) {
		return nil
	}
	return f.ctxs[i]
}

func (f *fakeProvider) CompleteStream(ctx context.Context, req aiprovider.Request) (<-chan aiprovider.StreamChunk, error) {
	f.mu.Lock()
	f.lastReq = req
	f.calls++
	f.reqs = append(f.reqs, req)
	f.ctxs = append(f.ctxs, ctx)
	if len(req.Messages) > 1 {
		f.usedImgs = len(req.Messages[1].Images)
	}
	tokens, errAt := f.tokens, f.errAt
	if byModel, ok := f.tokensByModel[req.Model]; ok {
		tokens = byModel
	}
	chunkDelay := f.firstChunkDelay
	if byModel, ok := f.firstChunkDelayByModel[req.Model]; ok {
		chunkDelay = byModel
	}
	var openErr error
	if len(f.openErrSeq) > 0 {
		openErr = f.openErrSeq[0]
		f.openErrSeq = f.openErrSeq[1:]
	} else {
		openErr = f.openErr
	}
	delay := f.openDelay
	rejectThinking := f.rejectThinking
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	if rejectThinking && req.ThinkingLevel != nil && !req.DisableThinking {
		return nil, aiprovider.ErrThinkingLevelRejected
	}
	if openErr != nil {
		return nil, openErr
	}
	f.mu.Lock()
	reason, block, usage := f.doneReason, f.doneBlock, f.doneUsage
	f.mu.Unlock()
	ch := make(chan aiprovider.StreamChunk, 32)
	go func() {
		defer close(ch)
		if chunkDelay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(chunkDelay):
			}
		}
		for i, tok := range tokens {
			if i == errAt {
				select {
				case ch <- aiprovider.StreamChunk{Err: errors.New("upstream exploded")}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case ch <- aiprovider.StreamChunk{Token: tok}:
			case <-ctx.Done():
				return
			}
		}
		if errAt == len(tokens) {
			select {
			case ch <- aiprovider.StreamChunk{Err: errors.New("upstream exploded")}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case ch <- aiprovider.StreamChunk{Done: true, FinishReason: reason, BlockReason: block, Usage: usage}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

// newFakeProvider scripts a stream emitting tokens then a normal Done
// (FinishReason "STOP"). errAt == -1 means no mid-stream error. Tests that
// need another terminal state set fp.doneReason (possibly "") directly.
func newFakeProvider(tokens ...string) *fakeProvider {
	return &fakeProvider{tokens: tokens, errAt: -1, doneReason: "STOP"}
}

// --- fake repository ---
// Mirrors the real postgresRepository contract, including the exact 409
// messages: the atomic UPDATE...WHERE in SQL is simulated with a mutex.
// (True cross-process atomicity can only be proven against a live DB.)

type fakeProject struct {
	project     model.Project
	versions    []*model.Version
	attachments []*model.ProjectAttachment
}

type fakeRepo struct {
	mu       sync.Mutex
	projects map[string]*fakeProject
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo { return &fakeRepo{projects: map[string]*fakeProject{}} }

func (f *fakeRepo) Create(_ context.Context, userID, title string, brief model.Brief) (*model.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &model.Project{
		ID:        uuid.NewString(),
		UserID:    userID,
		Title:     title,
		Status:    model.StatusDraft,
		Brief:     brief,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.projects[p.ID] = &fakeProject{project: *p}
	out := *p
	return &out, nil
}

func (f *fakeRepo) List(_ context.Context, userID string, limit, offset int) ([]*model.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*model.Project
	for _, fp := range f.projects {
		if fp.project.UserID == userID {
			cp := fp.project
			cp.VersionCount = len(fp.versions)
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if offset > len(out) {
		return []*model.Project{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (f *fakeRepo) FindByID(_ context.Context, userID, id string) (*model.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[id]
	if !ok || fp.project.UserID != userID {
		return nil, apperror.NotFound("Project not found", nil)
	}
	cp := fp.project
	cp.VersionCount = len(fp.versions)
	return &cp, nil
}

func (f *fakeRepo) FindWithLatest(_ context.Context, userID, id string) (*model.Project, *model.Version, error) {
	p, err := f.FindByID(context.Background(), userID, id)
	if err != nil {
		return nil, nil, err
	}
	if p.VersionCount == 0 {
		return p, nil, nil
	}
	latest, err := f.LatestVersion(context.Background(), p.ID)
	if err != nil {
		return nil, nil, err
	}
	return p, latest, nil
}

func (f *fakeRepo) Delete(_ context.Context, userID, id string) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[id]
	if !ok || fp.project.UserID != userID {
		return nil, false, apperror.NotFound("Project not found", nil)
	}
	if fp.project.Status == model.StatusGenerating {
		return nil, false, apperror.Conflict("Cannot delete project while generation is in progress", nil)
	}
	var paths []string
	for _, a := range fp.attachments {
		paths = append(paths, a.StoragePath)
	}
	delete(f.projects, id)
	return paths, true, nil
}

func (f *fakeRepo) UpdateTitle(_ context.Context, userID, id, title string) (*model.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[id]
	if !ok || fp.project.UserID != userID {
		return nil, apperror.NotFound("Project not found", nil)
	}
	if fp.project.Status == model.StatusGenerating {
		return nil, apperror.Conflict("Cannot rename project while generation is in progress", nil)
	}
	fp.project.Title = title
	fp.project.UpdatedAt = time.Now()
	cp := fp.project
	cp.VersionCount = len(fp.versions)
	return &cp, nil
}

func (f *fakeRepo) tryStart(userID, id string, allowed []string, alreadyHasVersionMsg string) (*model.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[id]
	if !ok || fp.project.UserID != userID {
		return nil, apperror.NotFound("Project not found", nil)
	}
	allowedSet := map[string]bool{}
	for _, s := range allowed {
		allowedSet[s] = true
	}
	if !allowedSet[fp.project.Status] {
		if fp.project.Status == model.StatusGenerating {
			return nil, apperror.Conflict("A generation is already in progress for this project", nil)
		}
		return nil, apperror.Conflict(alreadyHasVersionMsg, nil)
	}
	fp.project.Status = model.StatusGenerating
	fp.project.UpdatedAt = time.Now()
	cp := fp.project
	cp.VersionCount = len(fp.versions)
	return &cp, nil
}

func (f *fakeRepo) TryStartGenerate(_ context.Context, userID, id string) (*model.Project, error) {
	return f.tryStart(userID, id,
		[]string{model.StatusDraft, model.StatusFailed},
		"Project already has a version, use revise instead")
}

func (f *fakeRepo) TryStartRevise(_ context.Context, userID, id string) (*model.Project, error) {
	return f.tryStart(userID, id,
		[]string{model.StatusReady},
		"Project has no versions yet, use generate instead")
}

func (f *fakeRepo) FinishSuccess(_ context.Context, projectID, kind, prompt, html string) (*model.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok {
		return nil, apperror.NotFound("Project not found", nil)
	}
	maxNum := 0
	for _, v := range fp.versions {
		if v.VersionNumber > maxNum {
			maxNum = v.VersionNumber
		}
	}
	v := &model.Version{
		ID:            uuid.NewString(),
		ProjectID:     projectID,
		VersionNumber: maxNum + 1,
		Kind:          kind,
		Prompt:        prompt,
		HTML:          html,
		CreatedAt:     time.Now(),
	}
	fp.versions = append(fp.versions, v)
	fp.project.Status = model.StatusReady
	fp.project.UpdatedAt = time.Now()
	return v, nil
}

func (f *fakeRepo) FinishFailedGenerate(_ context.Context, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fp, ok := f.projects[projectID]; ok {
		fp.project.Status = model.StatusFailed
		fp.project.UpdatedAt = time.Now()
	}
	return nil
}

func (f *fakeRepo) FinishFailedRevise(_ context.Context, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fp, ok := f.projects[projectID]; ok {
		fp.project.Status = model.StatusReady
		fp.project.UpdatedAt = time.Now()
	}
	return nil
}

func (f *fakeRepo) LookupStatus(_ context.Context, userID, id string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[id]
	if !ok || fp.project.UserID != userID {
		return "", false, nil
	}
	return fp.project.Status, true, nil
}

func (f *fakeRepo) ListVersions(_ context.Context, userID, projectID string) ([]*model.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok || fp.project.UserID != userID {
		return nil, apperror.NotFound("Project not found", nil)
	}
	out := append([]*model.Version{}, fp.versions...)
	sort.Slice(out, func(i, j int) bool { return out[i].VersionNumber > out[j].VersionNumber })
	return out, nil
}

func (f *fakeRepo) FindVersion(_ context.Context, userID, projectID string, number int) (*model.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok || fp.project.UserID != userID {
		return nil, apperror.NotFound("Project not found", nil)
	}
	for _, v := range fp.versions {
		if v.VersionNumber == number {
			return v, nil
		}
	}
	return nil, apperror.NotFound("Version not found", nil)
}

func (f *fakeRepo) LatestVersion(_ context.Context, projectID string) (*model.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok || len(fp.versions) == 0 {
		return nil, apperror.NotFound("Version not found", nil)
	}
	best := fp.versions[0]
	for _, v := range fp.versions[1:] {
		if v.VersionNumber > best.VersionNumber {
			best = v
		}
	}
	return best, nil
}

func (f *fakeRepo) SaveAttachments(_ context.Context, projectID string, atts []*model.ProjectAttachment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok {
		return apperror.NotFound("Project not found", nil)
	}
	fp.attachments = append(fp.attachments, atts...)
	return nil
}

func (f *fakeRepo) ListAttachments(_ context.Context, userID, projectID string) ([]*model.ProjectAttachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok || fp.project.UserID != userID {
		return nil, apperror.NotFound("Project not found", nil)
	}
	return append([]*model.ProjectAttachment{}, fp.attachments...), nil
}

func (f *fakeRepo) FindAttachment(_ context.Context, userID, projectID, attachmentID string) (*model.ProjectAttachment, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.projects[projectID]
	if !ok || fp.project.UserID != userID {
		return nil, "", apperror.NotFound("Project not found", nil)
	}
	for _, a := range fp.attachments {
		if a.ID == attachmentID {
			return a, a.StoragePath, nil
		}
	}
	return nil, "", apperror.NotFound("Attachment not found", nil)
}

func (f *fakeRepo) ResetStuckGenerating(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, fp := range f.projects {
		if fp.project.Status == model.StatusGenerating {
			if len(fp.versions) > 0 {
				fp.project.Status = model.StatusReady
			} else {
				fp.project.Status = model.StatusFailed
			}
			n++
		}
	}
	return n, nil
}

// --- helpers ---

const testHTML = "<!DOCTYPE html><html lang=\"id\"><head><title>T</title></head><body><h1>Halo</h1></body></html>"

func newServiceWithFake(t *testing.T, fp *fakeProvider) (Service, *fakeRepo) {
	t.Helper()
	aiCfg, uploadCfg, projectCfg := testConfigs()
	uploadCfg.Dir = t.TempDir()
	repo := newFakeRepo()
	return New(repo, fp, aiCfg, uploadCfg, projectCfg, testLogger()), repo
}

func createDraft(t *testing.T, svc Service, userID string) *model.Project {
	t.Helper()
	p, err := svc.Create(context.Background(), userID, model.CreateProjectRequest{
		Brief: model.Brief{Description: "Landing page untuk kedai kopi lokal di Purwokerto dengan menu dan jam buka."},
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if p.Status != model.StatusDraft {
		t.Fatalf("new project status = %q, want draft", p.Status)
	}
	return p
}

// drainAndFinish simulates the handler SSE loop: relay chunks, then finalize.
// Like the handler, it records terminal metadata and stream counters on the
// pending stream.
func drainAndFinish(ctx context.Context, t *testing.T, svc Service, pending *PendingStream) (*model.Version, *apperror.Error) {
	t.Helper()
	var buf strings.Builder
	for chunk := range pending.Stream {
		pending.Chunks++
		if chunk.FinishReason != "" {
			pending.FinishReason = chunk.FinishReason
		}
		if chunk.BlockReason != "" {
			pending.BlockReason = chunk.BlockReason
		}
		if chunk.Usage != nil {
			pending.Usage = chunk.Usage
		}
		if chunk.Err != nil {
			svc.FailStream(ctx, pending)
			return nil, apperror.AIProvider("The AI provider returned an error", nil)
		}
		if chunk.Done {
			break
		}
		buf.WriteString(chunk.Token)
		pending.TextBytes += len(chunk.Token)
	}
	v, err := svc.FinishStream(ctx, pending, buf.String())
	if err != nil {
		var ae *apperror.Error
		if errors.As(err, &ae) {
			return nil, ae
		}
		t.Fatalf("unexpected error type: %v", err)
	}
	return v, nil
}

func appErr(t *testing.T, err error) *apperror.Error {
	t.Helper()
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apperror.Error, got %T (%v)", err, err)
	}
	return ae
}

// --- tests ---

func TestGenerateSuccessStoresVersionOne(t *testing.T) {
	fp := newFakeProvider("<!DOCTYPE html><html>", "<body>x</body></html>")
	svc, repo := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	v, ae := drainAndFinish(ctx, t, svc, pending)
	if ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	if v.VersionNumber != 1 || v.Kind != model.KindGenerate {
		t.Errorf("version = %+v, want number 1 kind generate", v)
	}
	if !strings.Contains(v.HTML, "<!DOCTYPE html>") {
		t.Errorf("stored html missing doctype: %q", v.HTML)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusReady || got.VersionCount != 1 {
		t.Errorf("after generate: status=%q count=%d, want ready/1", got.Status, got.VersionCount)
	}
	_ = repo
}

func TestGenerateMidStreamErrorLeavesFailed(t *testing.T) {
	fp := newFakeProvider("<html>", "<body>")
	fp.errAt = 1
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected provider error, got success")
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after failed generate: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestReviseSequenceAndBaseVersion(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("generate: %v", ae)
	}

	for want := 2; want <= 3; want++ {
		rp, err := svc.BeginRevise(ctx, "user-1", p.ID, "ganti warna hero jadi biru tua", nil, nil)
		if err != nil {
			t.Fatalf("BeginRevise: %v", err)
		}
		v, ae := drainAndFinish(ctx, t, svc, rp)
		if ae != nil {
			t.Fatalf("revise: %v", ae)
		}
		if v.VersionNumber != want || v.Kind != model.KindRevise {
			t.Errorf("version = %+v, want number %d kind revise", v, want)
		}
		if v.Prompt != "ganti warna hero jadi biru tua" {
			t.Errorf("revise prompt = %q", v.Prompt)
		}
	}

	missing := 99
	if _, err := svc.BeginRevise(ctx, "user-1", p.ID, "ubah lagi", &missing, nil); appErr(t, err).Code() != "NOT_FOUND" {
		t.Errorf("missing base_version err = %v, want NOT_FOUND", err)
	}
	// Failed revise rolls back to ready with versions intact.
	got, _, _ := svc.Get(ctx, "user-1", p.ID)
	if got.Status != model.StatusReady || got.VersionCount != 3 {
		t.Errorf("after bad base: status=%q count=%d, want ready/3", got.Status, got.VersionCount)
	}
}

func TestReviseMidStreamErrorKeepsReady(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, _ := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("generate: %v", ae)
	}

	fp.mu.Lock()
	fp.tokens = []string{"<partial"}
	fp.errAt = 1
	fp.mu.Unlock()
	rp, err := svc.BeginRevise(ctx, "user-1", p.ID, "ubah footer", nil, nil)
	if err != nil {
		t.Fatalf("BeginRevise: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, rp); ae == nil {
		t.Fatal("expected mid-stream error")
	}
	got, _, _ := svc.Get(ctx, "user-1", p.ID)
	if got.Status != model.StatusReady || got.VersionCount != 1 {
		t.Errorf("after failed revise: status=%q count=%d, want ready/1", got.Status, got.VersionCount)
	}
}

func TestUpdateTitle(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	got, err := svc.UpdateTitle(ctx, "user-1", p.ID, "  Judul Baru  ")
	if err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}
	if got.Title != "Judul Baru" {
		t.Errorf("title = %q, want trimmed Judul Baru", got.Title)
	}

	for _, tc := range []struct {
		name  string
		title string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"too long", strings.Repeat("a", 201)},
	} {
		if _, err := svc.UpdateTitle(ctx, "user-1", p.ID, tc.title); appErr(t, err).Code() != "VALIDATION_ERROR" {
			t.Errorf("%s: err = %v, want VALIDATION_ERROR", tc.name, err)
		}
	}
	if _, err := svc.UpdateTitle(ctx, "user-1", "not-a-uuid", "x"); appErr(t, err).Code() != "VALIDATION_ERROR" {
		t.Errorf("bad id: err = %v, want VALIDATION_ERROR", err)
	}

	// Foreign project reads as NOT_FOUND, never FORBIDDEN.
	if _, err := svc.UpdateTitle(ctx, "user-2", p.ID, "x"); appErr(t, err).Code() != "NOT_FOUND" {
		t.Errorf("foreign project: err = %v, want NOT_FOUND", err)
	}

	// Rename during generation is CONFLICT.
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, err := svc.UpdateTitle(ctx, "user-1", p.ID, "x"); appErr(t, err).Code() != "CONFLICT" {
		t.Errorf("generating project: err = %v, want CONFLICT", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	// Rename works again once ready.
	got, err = svc.UpdateTitle(ctx, "user-1", p.ID, "Sesudah Generate")
	if err != nil {
		t.Fatalf("UpdateTitle after ready: %v", err)
	}
	if got.Title != "Sesudah Generate" || got.VersionCount != 1 {
		t.Errorf("after ready: title=%q count=%d, want Sesudah Generate/1", got.Title, got.VersionCount)
	}
}

func TestStateMachineConflicts(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	// Revise on a draft: nothing to revise yet.
	if _, err := svc.BeginRevise(ctx, "user-1", p.ID, "ubah", nil, nil); appErr(t, err).Code() != "CONFLICT" {
		t.Errorf("revise-on-draft err = %v, want CONFLICT", err)
	}

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	// Second generate while the first is in flight.
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil); appErr(t, err).Code() != "CONFLICT" {
		t.Errorf("concurrent generate err = %v, want CONFLICT", err)
	} else if msg := appErr(t, err).Message; msg != "A generation is already in progress for this project" {
		t.Errorf("concurrent message = %q", msg)
	}
	// Delete while generating is rejected.
	if err := svc.Delete(ctx, "user-1", p.ID); appErr(t, err).Code() != "CONFLICT" {
		t.Errorf("delete-while-generating err = %v, want CONFLICT", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("generate: %v", ae)
	}

	// Generate on a project that already has versions.
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil); appErr(t, err).Code() != "CONFLICT" {
		t.Errorf("generate-on-ready err = %v, want CONFLICT", err)
	} else if msg := appErr(t, err).Message; msg != "Project already has a version, use revise instead" {
		t.Errorf("generate-on-ready message = %q", msg)
	}
}

func TestConcurrentGenerateOnlyOneWins(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	const n = 8
	var wg sync.WaitGroup
	wins := make(chan *PendingStream, n)
	conflicts := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
			if err != nil {
				conflicts <- err
				return
			}
			wins <- pending
		}()
	}
	wg.Wait()
	close(wins)
	close(conflicts)

	var pendings []*PendingStream
	for w := range wins {
		pendings = append(pendings, w)
	}
	var errs []error
	for e := range conflicts {
		errs = append(errs, e)
	}
	if len(pendings) != 1 {
		t.Fatalf("winners = %d, want exactly 1", len(pendings))
	}
	if len(errs) != n-1 {
		t.Fatalf("conflicts = %d, want %d", len(errs), n-1)
	}
	for _, e := range errs {
		if appErr(t, e).Code() != "CONFLICT" {
			t.Errorf("loser err = %v, want CONFLICT", e)
		}
	}
	// Settle the winner so no goroutine/stream leaks.
	if _, ae := drainAndFinish(ctx, t, svc, pendings[0]); ae != nil {
		t.Fatalf("winner finish: %v", ae)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-A")
	pending, _ := svc.BeginGenerate(ctx, "user-A", p.ID, nil)
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("generate: %v", ae)
	}

	for name, fn := range map[string]func() error{
		"get":           func() error { _, _, err := svc.Get(ctx, "user-B", p.ID); return err },
		"delete":        func() error { return svc.Delete(ctx, "user-B", p.ID) },
		"list versions": func() error { _, err := svc.ListVersions(ctx, "user-B", p.ID); return err },
		"get version":   func() error { _, err := svc.GetVersion(ctx, "user-B", p.ID, 1); return err },
		"export":        func() error { _, _, err := svc.ExportTarget(ctx, "user-B", p.ID, nil); return err },
		"list attach":   func() error { _, err := svc.ListAttachments(ctx, "user-B", p.ID); return err },
		"get attach":    func() error { _, _, err := svc.GetAttachment(ctx, "user-B", p.ID, uuid.NewString()); return err },
		"generate":      func() error { _, err := svc.BeginGenerate(ctx, "user-B", p.ID, nil); return err },
		"revise":        func() error { _, err := svc.BeginRevise(ctx, "user-B", p.ID, "x", nil, nil); return err },
		"missing id":    func() error { _, _, err := svc.Get(ctx, "user-A", uuid.NewString()); return err },
	} {
		if ae := appErr(t, fn()); ae.Code() != "NOT_FOUND" {
			t.Errorf("%s as user-B: code = %q, want NOT_FOUND", name, ae.Code())
		}
	}
}

func TestNilProviderFailsBeforeStream(t *testing.T) {
	aiCfg, uploadCfg, projectCfg := testConfigs()
	uploadCfg.Dir = t.TempDir()
	repo := newFakeRepo()
	svc := New(repo, nil, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p, err := svc.Create(ctx, "user-1", model.CreateProjectRequest{
		Brief: model.Brief{Description: "Landing page untuk kedai kopi lokal di Purwokerto dengan menu dan jam buka."},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil); appErr(t, err).Code() != "AI_PROVIDER_ERROR" {
		t.Fatalf("nil provider err = %v, want AI_PROVIDER_ERROR", err)
	}
	got, _, _ := svc.Get(ctx, "user-1", p.ID)
	if got.Status != model.StatusFailed {
		t.Errorf("status after provider open failure = %q, want failed", got.Status)
	}
}

func TestImageLimits(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	img := model.ImagePayload{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(png), FileName: "logo.png"}

	// Five images exceed the default PROJECT_MAX_IMAGES=4.
	many := []model.ImagePayload{img, img, img, img, img}
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, many); appErr(t, err).Code() != "VALIDATION_ERROR" {
		t.Errorf("too many images err = %v, want VALIDATION_ERROR", err)
	}
	bad := []model.ImagePayload{{MimeType: "image/png", Data: "!!!not-base64!!!"}}
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, bad); appErr(t, err).Code() != "VALIDATION_ERROR" {
		t.Errorf("bad base64 err = %v, want VALIDATION_ERROR", err)
	}
	gif := []model.ImagePayload{{MimeType: "image/gif", Data: base64.StdEncoding.EncodeToString(png)}}
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, gif); appErr(t, err).Code() != "VALIDATION_ERROR" {
		t.Errorf("gif mime err = %v, want VALIDATION_ERROR", err)
	}

	// One valid image is stored and forwarded to the model.
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, []model.ImagePayload{img})
	if err != nil {
		t.Fatalf("BeginGenerate with image: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	if fp.usedImgs != 1 {
		t.Errorf("provider received %d images, want 1", fp.usedImgs)
	}
	atts, err := svc.ListAttachments(ctx, "user-1", p.ID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments = %v, %v; want 1 row", atts, err)
	}
	if atts[0].MimeType != "image/png" || atts[0].FileType != "png" {
		t.Errorf("attachment = %+v, want png normalization", atts[0])
	}
}

func TestInvalidHTMLStoresNothing(t *testing.T) {
	fp := newFakeProvider("Maaf, saya tidak bisa membuat website.")
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected validation failure for non-HTML output")
	}
	got, _, _ := svc.Get(ctx, "user-1", p.ID)
	if got.VersionCount != 0 || got.Status != model.StatusFailed {
		t.Errorf("after invalid html: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestSystemPromptLocked(t *testing.T) {
	if !strings.Contains(SystemPrompt, "<!DOCTYPE html>") {
		t.Error("system prompt must pin the doctype-first output rule")
	}
	if !strings.Contains(SystemPrompt, "USER DATA") {
		t.Error("system prompt must mark user input as untrusted data")
	}
	gen := BuildGeneratePrompt("brief text here")
	if !strings.Contains(gen, "<brief>") || !strings.Contains(gen, "brief text here") {
		t.Errorf("generate prompt missing delimited brief: %q", gen)
	}
	rev := BuildRevisePrompt("brief", "<html></html>", "do the thing")
	for _, want := range []string{"<brief>", "<current_html>", "<revision_instruction>", "do the thing"} {
		if !strings.Contains(rev, want) {
			t.Errorf("revise prompt missing %q: %q", want, rev)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Website Kopi Senja":    "website-kopi-senja",
		"  A  B--C  ":           "a-b-c",
		"Kafe & Resto 24/7!":    "kafe-resto-24-7",
		"":                      "website",
		"../etc/passwd":         "etc-passwd",
		"Halo\x00\x01Dunia":     "halo-dunia",
		"Kopi Senja Purwokerto": "kopi-senja-purwokerto",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProviderRequestShape(t *testing.T) {
	fp := newFakeProvider(testHTML)
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	if len(fp.lastReq.Messages) != 2 || fp.lastReq.Messages[0].Role != "system" {
		t.Fatalf("provider request = %+v, want system+user messages", fp.lastReq.Messages)
	}
	if fp.lastReq.Messages[0].Content != SystemPrompt {
		t.Error("system message is not the locked SystemPrompt")
	}
}

func TestFinishStreamTruncatedOutputRejected(t *testing.T) {
	fp := newFakeProvider("<!DOCTYPE html><html><body>setengah")
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	// Simulate what the handler records from the Done chunk when Gemini
	// stops for MAX_TOKENS instead of STOP.
	pending.FinishReason = "MAX_TOKENS"
	_, err = svc.FinishStream(ctx, pending, "<!DOCTYPE html><html><body>setengah")
	ae := appErr(t, err)
	if ae.Code() != "AI_PROVIDER_ERROR" {
		t.Fatalf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
	}
	if !strings.Contains(ae.Message, "truncated") {
		t.Errorf("message = %q, want a clear truncation message", ae.Message)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after truncated generate: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestFinishStreamUnexpectedEndRejected(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.doneReason = "" // EOF without any finish reason.
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected unexpected-end error, got success")
	} else {
		if ae.Code() != "AI_PROVIDER_ERROR" {
			t.Fatalf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
		}
		if !strings.Contains(ae.Message, "unexpectedly") {
			t.Errorf("message = %q, want an unexpected-end message", ae.Message)
		}
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after unexpected end: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestFinishStreamBlockedRejected(t *testing.T) {
	fp := newFakeProvider("<!DOCTYPE html><html><body>x</body></html>")
	fp.doneReason = "SAFETY"
	fp.doneBlock = "SAFETY"
	fp.doneUsage = &aiprovider.UsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5, TotalTokenCount: 15}
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected blocked error, got success")
	} else {
		if ae.Code() != "AI_PROVIDER_ERROR" {
			t.Fatalf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
		}
		if !strings.Contains(ae.Message, "blocked") {
			t.Errorf("message = %q, want a blocked message", ae.Message)
		}
	}
	if pending.BlockReason != "SAFETY" {
		t.Errorf("pending BlockReason = %q, want SAFETY", pending.BlockReason)
	}
	if pending.Usage == nil || pending.Usage.TotalTokenCount != 15 {
		t.Errorf("pending Usage = %+v, want total 15", pending.Usage)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after blocked generate: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestFinishStreamBlockedViaBlockReasonOnly(t *testing.T) {
	// A promptFeedback block with no candidates (empty finishReason) must
	// still classify as blocked, not as a generic unexpected end.
	fp := newFakeProvider(testHTML)
	fp.doneReason = ""
	fp.doneBlock = "BLOCKLIST"
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected blocked error, got success")
	} else {
		if ae.Code() != "AI_PROVIDER_ERROR" {
			t.Fatalf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
		}
		if !strings.Contains(ae.Message, "blocked") {
			t.Errorf("message = %q, want a blocked message", ae.Message)
		}
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after blockReason-only generate: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestFinishStreamOtherReasonIsUnexpected(t *testing.T) {
	// OTHER is neither STOP, MAX_TOKENS nor a block: it must surface as an
	// unexpected end with no version stored.
	fp := newFakeProvider(testHTML)
	fp.doneReason = "OTHER"
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected unexpected-end error, got success")
	} else {
		if ae.Code() != "AI_PROVIDER_ERROR" {
			t.Fatalf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
		}
		if !strings.Contains(ae.Message, "unexpectedly") {
			t.Errorf("message = %q, want an unexpected-end message", ae.Message)
		}
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after OTHER generate: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestFinishStreamReasons(t *testing.T) {
	// Each FinishStream failure class must carry its stable reason for the
	// client envelope; the code stays AI_PROVIDER_ERROR in all cases.
	for _, tc := range []struct {
		name       string
		tokens     []string
		doneReason string
		doneBlock  string
		wantReason string
	}{
		{"truncated", []string{"<!DOCTYPE html><html><body>setengah"}, "MAX_TOKENS", "", apperror.ReasonOutputTruncated},
		{"blocked", []string{"<!DOCTYPE html><html><body>x</body></html>"}, "SAFETY", "SAFETY", apperror.ReasonOutputBlocked},
		{"interrupted", []string{testHTML}, "", "", apperror.ReasonStreamInterrupted},
		{"invalid", []string{"Maaf, saya tidak bisa membuat website."}, "STOP", "", apperror.ReasonInvalidOutput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fp := newFakeProvider(tc.tokens...)
			fp.doneReason = tc.doneReason
			fp.doneBlock = tc.doneBlock
			svc, _ := newServiceWithFake(t, fp)
			ctx := context.Background()

			p := createDraft(t, svc, "user-1")
			pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
			if err != nil {
				t.Fatalf("BeginGenerate: %v", err)
			}
			_, ae := drainAndFinish(ctx, t, svc, pending)
			if ae == nil {
				t.Fatal("expected failure, got success")
			}
			if ae.Code() != "AI_PROVIDER_ERROR" {
				t.Errorf("code = %q, want AI_PROVIDER_ERROR", ae.Code())
			}
			if ae.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", ae.Reason, tc.wantReason)
			}
		})
	}
}

// --- limited pre-stream retry (AI_RETRY_ON_UNAVAILABLE) ---

func overloadedOpenErr() error {
	return apperror.AIProvider("The AI provider returned an error", errors.New("status 503"))
}

func newRetryService(t *testing.T, fp *fakeProvider, retries int) (Service, *fakeRepo) {
	t.Helper()
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.RetryOnUnavailable = retries
	aiCfg.RetryBackoff = time.Millisecond
	uploadCfg.Dir = t.TempDir()
	repo := newFakeRepo()
	return New(repo, fp, aiCfg, uploadCfg, projectCfg, testLogger()), repo
}

func TestOpenRetrySucceedsOnSecondAttempt(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openErrSeq = []error{overloadedOpenErr(), nil}
	svc, _ := newRetryService(t, fp, 2)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (one retry)", fp.calls)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusReady || got.VersionCount != 1 {
		t.Errorf("after retried generate: status=%q count=%d, want ready/1", got.Status, got.VersionCount)
	}
}

func TestOpenRetryDisabledByDefault(t *testing.T) {
	// Default 0 must behave exactly as before: one call, no retry.
	fp := newFakeProvider(testHTML)
	fp.openErr = overloadedOpenErr()
	svc, _ := newServiceWithFake(t, fp)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil); err == nil {
		t.Fatal("expected provider error, got success")
	}
	if fp.calls != 1 {
		t.Errorf("provider calls = %d, want exactly 1 (no retry when disabled)", fp.calls)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after failed generate: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestOpenRetryExhaustedStaysBounded(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openErr = overloadedOpenErr()
	svc, _ := newRetryService(t, fp, 1)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	_, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err == nil {
		t.Fatal("expected provider error, got success")
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "AI_BUSY" {
		t.Errorf("error = %v, want AI_BUSY after exhausted retries", err)
	}
	if ae.Message != OpenBusyMessage {
		t.Errorf("message = %q, want the busy message", ae.Message)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (initial + 1 retry), never more", fp.calls)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after exhausted retries: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestNoRetryForMidStreamError(t *testing.T) {
	// Retry applies to the open call only: a mid-stream failure must not
	// trigger another provider call even when retries are enabled.
	fp := newFakeProvider("<html>", "<body>")
	fp.errAt = 1
	svc, _ := newRetryService(t, fp, 2)
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae == nil {
		t.Fatal("expected mid-stream error, got success")
	}
	if fp.calls != 1 {
		t.Errorf("provider calls = %d, want 1 (no retry for mid-stream errors)", fp.calls)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("after mid-stream error: status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestRetryStopsOnCancelledContext(t *testing.T) {
	// A cancelled client context must prevent any provider call, even with
	// retries enabled.
	fp := newFakeProvider(testHTML)
	fp.openErrSeq = []error{overloadedOpenErr(), nil}
	svc, _ := newRetryService(t, fp, 2)

	p := createDraft(t, svc, "user-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil); err == nil {
		t.Fatal("expected cancellation error, got success")
	}
	if fp.calls != 0 {
		t.Errorf("provider calls = %d, want 0 (cancelled before first attempt)", fp.calls)
	}
	got, _, err := svc.Get(context.Background(), "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.VersionCount != 0 {
		t.Errorf("version count = %d, want 0 (nothing stored)", got.VersionCount)
	}
}

// --- model fallback chain ---

func TestOpenFallbackOnNonRetryableErrorSucceedsWithNextModel(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openErrSeq = []error{apperror.AIProviderReason(apperror.ReasonProviderError, "The AI provider returned an error", errors.New("status 404")), nil}

	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	uploadCfg.Dir = t.TempDir()
	repo := newFakeRepo()
	svc := New(repo, fp, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (primary failed with 404, fallback succeeded)", fp.calls)
	}
	if fp.lastReq.Model != "fallback-model" {
		t.Errorf("lastReq.Model = %q, want fallback-model", fp.lastReq.Model)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusReady || got.VersionCount != 1 {
		t.Errorf("status=%q count=%d, want ready/1", got.Status, got.VersionCount)
	}
}

func TestOpenFallbackOnOverloadExhaustedSucceedsWithNextModel(t *testing.T) {
	fp := newFakeProvider(testHTML)
	// Primary model exhausts 1 retry (2 calls total), then fallback model succeeds on 1st call.
	fp.openErrSeq = []error{overloadedOpenErr(), overloadedOpenErr(), nil}

	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	aiCfg.RetryOnUnavailable = 1
	aiCfg.RetryBackoff = time.Millisecond
	uploadCfg.Dir = t.TempDir()
	repo := newFakeRepo()
	svc := New(repo, fp, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 3 {
		t.Errorf("provider calls = %d, want 3 (2 on primary, 1 on fallback)", fp.calls)
	}
	if fp.lastReq.Model != "fallback-model" {
		t.Errorf("lastReq.Model = %q, want fallback-model", fp.lastReq.Model)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusReady || got.VersionCount != 1 {
		t.Errorf("status=%q count=%d, want ready/1", got.Status, got.VersionCount)
	}
}

func TestOpenFallbackAllModelsFailReturnsLastError(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openErr = errors.New("upstream provider exploded")

	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model-1", "fallback-model-2"}
	uploadCfg.Dir = t.TempDir()
	repo := newFakeRepo()
	svc := New(repo, fp, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	_, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err == nil {
		t.Fatal("expected error when all models fail, got nil")
	}
	if fp.calls != 3 {
		t.Errorf("provider calls = %d, want 3 (attempted all 3 models)", fp.calls)
	}
	got, _, err := svc.Get(ctx, "user-1", p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != model.StatusFailed || got.VersionCount != 0 {
		t.Errorf("status=%q count=%d, want failed/0", got.Status, got.VersionCount)
	}
}

func TestOpenHeaderTimeoutSkipsSameModelRetry(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openErrSeq = []error{aiprovider.ErrOpenTimeout, nil}

	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	aiCfg.RetryOnUnavailable = 2
	aiCfg.RetryBackoff = time.Millisecond
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (no same-model retry on header timeout)", fp.calls)
	}
	if fp.lastReq.Model != "fallback-model" {
		t.Errorf("lastReq.Model = %q, want fallback-model", fp.lastReq.Model)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
}

func TestOpenHeaderTimeoutAllModelsReturnsAIBusy(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openErr = aiprovider.ErrOpenTimeout

	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	aiCfg.RetryOnUnavailable = 2
	aiCfg.RetryBackoff = time.Millisecond
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger())

	p := createDraft(t, svc, "user-1")
	_, err := svc.BeginGenerate(context.Background(), "user-1", p.ID, nil)
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "AI_BUSY" {
		t.Fatalf("error = %v, want AI_BUSY", err)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (one per model, no same-model retry)", fp.calls)
	}
}

func TestOpenBudgetExhaustionStopsFallbackChain(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.openDelay = 500 * time.Millisecond

	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	aiCfg.RetryOnUnavailable = 1
	aiCfg.RetryBackoff = time.Millisecond
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger())

	p := createDraft(t, svc, "user-1")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("open took %v, want bounded by the context budget", elapsed)
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "AI_BUSY" {
		t.Fatalf("error = %v, want AI_BUSY", err)
	}
	if fp.calls != 1 {
		t.Errorf("provider calls = %d, want 1 (budget spent on first model, no fallback)", fp.calls)
	}
}

func TestOpenThinkingLevelRejectedRetriesOnceWithoutIt(t *testing.T) {
	fp := newFakeProvider(testHTML)
	fp.rejectThinking = true

	aiCfg, uploadCfg, projectCfg := testConfigs()
	level := "LOW"
	aiCfg.ThinkingLevel = &level
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (rejected, then retried without thinking)", fp.calls)
	}
	if fp.reqs[0].ThinkingLevel == nil || *fp.reqs[0].ThinkingLevel != "LOW" {
		t.Errorf("first request thinking = %v, want LOW", fp.reqs[0].ThinkingLevel)
	}
	if fp.reqs[1].ThinkingLevel != nil || !fp.reqs[1].DisableThinking {
		t.Errorf("retry thinking = %v disable=%v, want omitted/disabled",
			fp.reqs[1].ThinkingLevel, fp.reqs[1].DisableThinking)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
}

func TestUnsupportedThinkingLevelIsNotSent(t *testing.T) {
	fp := newFakeProvider(testHTML)
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "gemini-3.8-flash"
	level := "MINIMAL"
	aiCfg.ThinkingLevel = &level
	uploadCfg.Dir = t.TempDir()
	svc := New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger())
	ctx := context.Background()

	p := createDraft(t, svc, "user-1")
	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", fp.calls)
	}
	if fp.reqs[0].ThinkingLevel != nil {
		t.Errorf("thinking = %v, want omitted (MINIMAL unsupported on gemini-3.8-flash)", fp.reqs[0].ThinkingLevel)
	}
	if _, ae := drainAndFinish(ctx, t, svc, pending); ae != nil {
		t.Fatalf("finish: %v", ae)
	}
}

// --- hedging ---

func newHedgeService(t *testing.T, fp *fakeProvider, mutate func(*config.AIConfig)) Service {
	t.Helper()
	aiCfg, uploadCfg, projectCfg := testConfigs()
	aiCfg.Model = "primary-model"
	aiCfg.FallbackModels = []string{"fallback-model"}
	aiCfg.RetryOnUnavailable = 0
	aiCfg.RetryBackoff = time.Millisecond
	if mutate != nil {
		mutate(&aiCfg)
	}
	uploadCfg.Dir = t.TempDir()
	return New(newFakeRepo(), fp, aiCfg, uploadCfg, projectCfg, testLogger())
}

func collectTokens(t *testing.T, ch <-chan aiprovider.StreamChunk) []string {
	t.Helper()
	var tokens []string
	deadline := time.After(3 * time.Second)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return tokens
			}
			if chunk.Err != nil {
				t.Fatalf("stream error: %v", chunk.Err)
			}
			if chunk.Token != "" {
				tokens = append(tokens, chunk.Token)
			}
		case <-deadline:
			t.Fatal("timed out draining stream")
		}
	}
}

func TestHedgeFallbackWinsWhenPrimaryIsSlow(t *testing.T) {
	fp := newFakeProvider()
	fp.tokensByModel = map[string][]string{
		"primary-model":  {"P1", "P2"},
		"fallback-model": {"F1", "F2"},
	}
	fp.firstChunkDelayByModel = map[string]time.Duration{
		"primary-model":  300 * time.Millisecond,
		"fallback-model": 10 * time.Millisecond,
	}
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 30 * time.Millisecond
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if !pending.FallbackUsed || pending.HedgeWinner != "fallback" {
		t.Errorf("fallback_used=%v hedge_winner=%q, want true/fallback", pending.FallbackUsed, pending.HedgeWinner)
	}
	if !pending.LoserCancelled || pending.HedgeStarted <= 0 {
		t.Errorf("loser_cancelled=%v hedge_started=%v, want true and >0", pending.LoserCancelled, pending.HedgeStarted)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2", fp.calls)
	}
	if got := collectTokens(t, pending.Stream); len(got) != 2 || got[0] != "F1" || got[1] != "F2" {
		t.Errorf("tokens = %v, want [F1 F2]", got)
	}
	svc.FailStream(ctx, pending)

	waitFor(t, time.Second, func() bool { return fp.contextAt(0).Err() == context.Canceled })
}

func TestHedgeNotStartedWhenPrimaryIsFast(t *testing.T) {
	fp := newFakeProvider("P1")
	fp.firstChunkDelay = 5 * time.Millisecond
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 500 * time.Millisecond
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if pending.HedgeWinner != "" || pending.HedgeStarted != 0 {
		t.Errorf("hedge_winner=%q hedge_started=%v, want none", pending.HedgeWinner, pending.HedgeStarted)
	}
	if fp.calls != 1 {
		t.Errorf("provider calls = %d, want 1 (fallback never started)", fp.calls)
	}
	collectTokens(t, pending.Stream)
	svc.FailStream(ctx, pending)
}

func TestHedgePrimaryOverloadRetriesThenFallbackOnce(t *testing.T) {
	fp := newFakeProvider("F1")
	fp.openErrSeq = []error{overloadedOpenErr(), overloadedOpenErr(), nil}
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = time.Second // long: fallback starts from primary failure, not the timer
		c.RetryOnUnavailable = 1
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 3 {
		t.Fatalf("provider calls = %d, want 3 (2 primary, 1 fallback)", fp.calls)
	}
	if fp.reqs[0].Model != "primary-model" || fp.reqs[1].Model != "primary-model" || fp.reqs[2].Model != "fallback-model" {
		t.Errorf("model order = [%s %s %s], want primary, primary, fallback",
			fp.reqs[0].Model, fp.reqs[1].Model, fp.reqs[2].Model)
	}
	if pending.HedgeWinner != "" {
		t.Errorf("hedge_winner = %q, want empty (timer never fired)", pending.HedgeWinner)
	}
	if !pending.FallbackUsed {
		t.Error("fallback_used = false, want true")
	}
	collectTokens(t, pending.Stream)
	svc.FailStream(ctx, pending)
}

func TestHedgeRespectsOpenBudgetWhenBothSlow(t *testing.T) {
	fp := newFakeProvider("X")
	fp.firstChunkDelay = 5 * time.Second
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 10 * time.Millisecond
	})
	p := createDraft(t, svc, "user-1")

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("open took %v, want bounded by the context budget", elapsed)
	}
	var ae *apperror.Error
	if !errors.As(err, &ae) || ae.Code() != "AI_BUSY" {
		t.Fatalf("error = %v, want AI_BUSY", err)
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (primary + hedged fallback)", fp.calls)
	}
}

func TestHedgeDoesNotLeakLoserGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	fp := newFakeProvider()
	fp.tokensByModel = map[string][]string{
		"primary-model":  {"P1"},
		"fallback-model": {"F1"},
	}
	fp.firstChunkDelayByModel = map[string]time.Duration{
		"primary-model":  300 * time.Millisecond,
		"fallback-model": 5 * time.Millisecond,
	}
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 20 * time.Millisecond
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	collectTokens(t, pending.Stream)
	svc.FailStream(ctx, pending)

	time.Sleep(150 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Errorf("goroutines before=%d after=%d, want no leaked hedge workers", before, after)
	}
	if fp.contextAt(0).Err() != context.Canceled {
		t.Errorf("loser ctx err = %v, want context.Canceled", fp.contextAt(0).Err())
	}
}

func TestHedgeNeverRelaysLoserTokens(t *testing.T) {
	fp := newFakeProvider()
	fp.tokensByModel = map[string][]string{
		"primary-model":  {"P1", "P2"},
		"fallback-model": {"F1", "F2"},
	}
	fp.firstChunkDelayByModel = map[string]time.Duration{
		"primary-model":  200 * time.Millisecond,
		"fallback-model": 5 * time.Millisecond,
	}
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 20 * time.Millisecond
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	got := collectTokens(t, pending.Stream)
	if len(got) != 2 || got[0] != "F1" || got[1] != "F2" {
		t.Errorf("tokens = %v, want exactly the fallback tokens [F1 F2]", got)
	}
	for _, tok := range got {
		if strings.HasPrefix(tok, "P") {
			t.Errorf("loser token %q reached the stream", tok)
		}
	}
	svc.FailStream(ctx, pending)
}

func TestHedgePrimaryWinsAfterHedgeFires(t *testing.T) {
	fp := newFakeProvider()
	fp.tokensByModel = map[string][]string{
		"primary-model":  {"P1"},
		"fallback-model": {"F1"},
	}
	fp.firstChunkDelayByModel = map[string]time.Duration{
		"primary-model":  80 * time.Millisecond,
		"fallback-model": 300 * time.Millisecond,
	}
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 20 * time.Millisecond
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if pending.HedgeWinner != "primary" || !pending.LoserCancelled {
		t.Errorf("hedge_winner=%q loser_cancelled=%v, want primary/true", pending.HedgeWinner, pending.LoserCancelled)
	}
	if got := collectTokens(t, pending.Stream); len(got) != 1 || got[0] != "P1" {
		t.Errorf("tokens = %v, want [P1]", got)
	}
	svc.FailStream(ctx, pending)
	waitFor(t, time.Second, func() bool { return fp.contextAt(1).Err() == context.Canceled })
}

func TestHedgeWorksWithThinkingLevelRetry(t *testing.T) {
	fp := newFakeProvider()
	fp.rejectThinking = true
	fp.tokensByModel = map[string][]string{
		"primary-model":  {"P1"},
		"fallback-model": {"F1"},
	}
	fp.firstChunkDelayByModel = map[string]time.Duration{
		"primary-model":  200 * time.Millisecond,
		"fallback-model": 5 * time.Millisecond,
	}
	level := "LOW"
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.HedgeAfter = 50 * time.Millisecond
		c.ThinkingLevel = &level
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 4 {
		t.Fatalf("provider calls = %d, want 4 (two per model: reject + retry)", fp.calls)
	}
	perModel := map[string]int{}
	retried := map[string]int{}
	for _, req := range fp.reqs {
		perModel[req.Model]++
		if req.ThinkingLevel == nil {
			if !req.DisableThinking {
				t.Errorf("model %s retry missing DisableThinking", req.Model)
			}
			retried[req.Model]++
		}
	}
	if perModel["primary-model"] != 2 || perModel["fallback-model"] != 2 {
		t.Errorf("calls per model = %v, want 2/2", perModel)
	}
	if retried["primary-model"] != 1 || retried["fallback-model"] != 1 {
		t.Errorf("thinking retries per model = %v, want one each", retried)
	}
	if pending.HedgeWinner != "fallback" {
		t.Errorf("hedge_winner = %q, want fallback", pending.HedgeWinner)
	}
	collectTokens(t, pending.Stream)
	svc.FailStream(ctx, pending)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestHedgeRemainingFallbackAfterBothCandidatesFail(t *testing.T) {
	fp := newFakeProvider("F2")
	fp.openErrSeq = []error{overloadedOpenErr(), overloadedOpenErr(), nil}
	svc := newHedgeService(t, fp, func(c *config.AIConfig) {
		c.FallbackModels = []string{"fallback-model", "fallback-model-2"}
		c.HedgeAfter = 10 * time.Millisecond
	})
	ctx := context.Background()
	p := createDraft(t, svc, "user-1")

	pending, err := svc.BeginGenerate(ctx, "user-1", p.ID, nil)
	if err != nil {
		t.Fatalf("BeginGenerate: %v", err)
	}
	if fp.calls != 3 {
		t.Fatalf("provider calls = %d, want 3", fp.calls)
	}
	if fp.reqs[2].Model != "fallback-model-2" {
		t.Errorf("third call model = %q, want fallback-model-2", fp.reqs[2].Model)
	}
	if pending.Model != "fallback-model-2" || !pending.FallbackUsed {
		t.Errorf("model=%q fallback_used=%v, want fallback-model-2/true", pending.Model, pending.FallbackUsed)
	}
	collectTokens(t, pending.Stream)
	svc.FailStream(ctx, pending)
}
