package service

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
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
	mu       sync.Mutex
	tokens   []string
	errAt    int // emit Err instead of token at this index (-1 = no error)
	openErr  error
	lastReq  aiprovider.Request
	calls    int
	usedImgs int
}

func (f *fakeProvider) Complete(_ context.Context, req aiprovider.Request) (aiprovider.Response, error) {
	return aiprovider.Response{Content: "unused", Model: "test"}, nil
}

func (f *fakeProvider) CompleteStream(_ context.Context, req aiprovider.Request) (<-chan aiprovider.StreamChunk, error) {
	f.mu.Lock()
	f.lastReq = req
	f.calls++
	if len(req.Messages) > 1 {
		f.usedImgs = len(req.Messages[1].Images)
	}
	tokens, errAt, openErr := f.tokens, f.errAt, f.openErr
	f.mu.Unlock()
	if openErr != nil {
		return nil, openErr
	}
	ch := make(chan aiprovider.StreamChunk, 32)
	go func() {
		defer close(ch)
		for i, tok := range tokens {
			if i == errAt {
				ch <- aiprovider.StreamChunk{Err: errors.New("upstream exploded")}
				return
			}
			ch <- aiprovider.StreamChunk{Token: tok}
		}
		if errAt == len(tokens) {
			ch <- aiprovider.StreamChunk{Err: errors.New("upstream exploded")}
			return
		}
		ch <- aiprovider.StreamChunk{Done: true}
	}()
	return ch, nil
}

func (f *fakeProvider) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return make([]float32, 8), nil
}

// newFakeProvider scripts a stream emitting tokens then Done.
// errAt == -1 means no mid-stream error.
func newFakeProvider(tokens ...string) *fakeProvider {
	return &fakeProvider{tokens: tokens, errAt: -1}
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
func drainAndFinish(ctx context.Context, t *testing.T, svc Service, pending *PendingStream) (*model.Version, *apperror.Error) {
	t.Helper()
	var buf strings.Builder
	for chunk := range pending.Stream {
		if chunk.Err != nil {
			svc.FailStream(ctx, pending)
			return nil, apperror.AIProvider("The AI provider returned an error", nil)
		}
		if chunk.Done {
			break
		}
		buf.WriteString(chunk.Token)
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
