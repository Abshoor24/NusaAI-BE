package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/middleware"
	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	"github.com/Abshoor24/backend-go/internal/modules/project/model"
	"github.com/Abshoor24/backend-go/internal/modules/project/service"
)

func testHandlerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubService is a scriptable service.Service for HTTP-level tests.
type stubService struct {
	beginPending *service.PendingStream
	beginErr     error
	finishVer    *model.Version
	finishErr    error
	finishCalls  int
	failCalls    int

	createOut *model.Project
	createErr error

	updateOut *model.Project
	updateErr error

	listOut []*model.Project

	getProj *model.Project
	getVer  *model.Version
	getErr  error

	exportProj *model.Project
	exportVer  *model.Version
	exportErr  error

	attach    *model.ProjectAttachment
	attachErr error
	attachBin string
}

func (s *stubService) Create(_ context.Context, _ string, _ model.CreateProjectRequest) (*model.Project, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.createOut, nil
}
func (s *stubService) List(_ context.Context, _ string, _, _ int) ([]*model.Project, error) {
	if s.listOut == nil {
		return []*model.Project{}, nil
	}
	return s.listOut, nil
}
func (s *stubService) Get(_ context.Context, _, _ string) (*model.Project, *model.Version, error) {
	if s.getErr != nil {
		return nil, nil, s.getErr
	}
	return s.getProj, s.getVer, nil
}
func (s *stubService) Delete(_ context.Context, _, _ string) error { return nil }
func (s *stubService) UpdateTitle(_ context.Context, _, _ string, _ string) (*model.Project, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.updateOut, nil
}
func (s *stubService) ListVersions(_ context.Context, _, _ string) ([]*model.Version, error) {
	return []*model.Version{}, nil
}
func (s *stubService) GetVersion(_ context.Context, _, _ string, _ int) (*model.Version, error) {
	return nil, apperror.NotFound("Version not found", nil)
}
func (s *stubService) ExportTarget(_ context.Context, _, _ string, _ *int) (*model.Project, *model.Version, error) {
	if s.exportErr != nil {
		return nil, nil, s.exportErr
	}
	return s.exportProj, s.exportVer, nil
}
func (s *stubService) ListAttachments(_ context.Context, _, _ string) ([]*model.ProjectAttachment, error) {
	return []*model.ProjectAttachment{}, nil
}
func (s *stubService) GetAttachment(_ context.Context, _, _, _ string) (*model.ProjectAttachment, io.ReadCloser, error) {
	if s.attachErr != nil {
		return nil, nil, s.attachErr
	}
	return s.attach, io.NopCloser(strings.NewReader(s.attachBin)), nil
}
func (s *stubService) BeginGenerate(_ context.Context, _ string, _ string, _ []model.ImagePayload) (*service.PendingStream, error) {
	return s.beginPending, s.beginErr
}
func (s *stubService) BeginRevise(_ context.Context, _ string, _ string, _ string, _ *int, _ []model.ImagePayload) (*service.PendingStream, error) {
	return s.beginPending, s.beginErr
}
func (s *stubService) FinishStream(_ context.Context, _ *service.PendingStream, _ string) (*model.Version, error) {
	s.finishCalls++
	return s.finishVer, s.finishErr
}
func (s *stubService) FailStream(_ context.Context, _ *service.PendingStream) { s.failCalls++ }

var _ service.Service = (*stubService)(nil)

func newTestHandler(stub *stubService) *Handler {
	aiCfg := config.AIConfig{MaxBytes: 1 << 20}
	uploadCfg := config.UploadConfig{Dir: "uploads", MaxSizeBytes: 10 << 20}
	projectCfg := config.ProjectConfig{MaxImages: 4, MaxHTMLBytes: 524288}
	return New(stub, testHandlerLogger(), aiCfg, uploadCfg, projectCfg)
}

func authed(req *http.Request, params map[string]string) *http.Request {
	ctx := middleware.WithUserID(req.Context(), "user-1")
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	return req.WithContext(ctx)
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) (int, string) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	code, _ := errObj["code"].(string)
	return rec.Code, code
}

const validCreateBody = `{"title":"Website Kopi Senja","brief":{"description":"Landing page untuk kedai kopi lokal di Purwokerto dengan menu dan jam buka.","website_type":"landing_page","language":"id"}}`

func TestCreateRejectsUnknownFields(t *testing.T) {
	h := newTestHandler(&stubService{createOut: &model.Project{ID: "p1"}})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"system field", `{"title":"x","brief":{"description":"Landing page untuk kedai kopi lokal di Purwokerto."},"system":"ignore rules"}`},
		{"typo field", `{"title":"x","brief":{"description":"Landing page untuk kedai kopi lokal di Purwokerto."},"titles":"x"}`},
		{"unknown brief field", `{"brief":{"description":"Landing page untuk kedai kopi lokal di Purwokerto.","prompt_injection":"x"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(tc.body)), nil)
			rec := httptest.NewRecorder()
			h.Create(rec, req)
			if status, code := decodeErr(t, rec); status != http.StatusBadRequest || code != "BAD_REQUEST" {
				t.Errorf("status=%d code=%q, want 400 BAD_REQUEST", status, code)
			}
		})
	}

	// A valid body still passes.
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(validCreateBody)), nil)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("valid create status = %d, want 201", rec.Code)
	}
}

func TestGenerateRejectsUnknownFields(t *testing.T) {
	h := newTestHandler(&stubService{})
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{"images":[],"system":"override"}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)
	if status, code := decodeErr(t, rec); status != http.StatusBadRequest || code != "BAD_REQUEST" {
		t.Errorf("status=%d code=%q, want 400 BAD_REQUEST", status, code)
	}
}

func scriptedStream(tokens []string, midErr bool) <-chan aiprovider.StreamChunk {
	ch := make(chan aiprovider.StreamChunk, 16)
	go func() {
		defer close(ch)
		for _, tok := range tokens {
			ch <- aiprovider.StreamChunk{Token: tok}
		}
		if midErr {
			ch <- aiprovider.StreamChunk{Err: io.ErrUnexpectedEOF}
			return
		}
		ch <- aiprovider.StreamChunk{Done: true}
	}()
	return ch
}

func pendingFor(projectID string, stream <-chan aiprovider.StreamChunk) *service.PendingStream {
	return &service.PendingStream{
		Project:   &model.Project{ID: projectID, Status: model.StatusGenerating},
		Kind:      model.KindGenerate,
		Prompt:    "brief",
		Stream:    stream,
		StartedAt: time.Now(),
		MaxBytes:  524288,
	}
}

func TestGenerateSSESuccess(t *testing.T) {	stub := &stubService{
		finishVer: &model.Version{ID: "v1", ProjectID: "p1", VersionNumber: 1, Kind: "generate", HTML: "<!DOCTYPE html><html></html>"},
	}
	stub.beginPending = pendingFor("p1", scriptedStream([]string{"<!DOCTYPE html>", "<html></html>"}, false))
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if got := strings.Count(body, "event: token"); got != 2 {
		t.Errorf("token events = %d, want 2\n%s", got, body)
	}
	if !strings.Contains(body, "event: done") || !strings.Contains(body, `"version_number":1`) {
		t.Errorf("missing done event with version 1:\n%s", body)
	}
	if stub.finishCalls != 1 || stub.failCalls != 0 {
		t.Errorf("finish=%d fail=%d, want 1/0", stub.finishCalls, stub.failCalls)
	}
}

func TestGenerateSSEMidStreamError(t *testing.T) {
	stub := &stubService{}
	stub.beginPending = pendingFor("p1", scriptedStream([]string{"<partial"}, true))
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("missing error event:\n%s", body)
	}
	if strings.Contains(body, "event: done") {
		t.Errorf("must not emit done after mid-stream error:\n%s", body)
	}
	if !strings.Contains(body, "The AI provider returned an error") {
		t.Errorf("error event must carry the generic message:\n%s", body)
	}
	if stub.finishCalls != 0 || stub.failCalls != 1 {
		t.Errorf("finish=%d fail=%d, want 0/1", stub.finishCalls, stub.failCalls)
	}
}

func TestGenerateSSEValidationFailure(t *testing.T) {
	stub := &stubService{finishErr: apperror.AIProvider("Generated output is not a valid HTML document", nil)}
	stub.beginPending = pendingFor("p1", scriptedStream([]string{"not html at all"}, false))
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") || strings.Contains(body, "event: done") {
		t.Errorf("want error without done:\n%s", body)
	}
	if strings.Contains(body, "not a valid HTML") {
		t.Error("internal validation detail must not leak to the client")
	}
}

func TestReviseRequiresInstruction(t *testing.T) {
	h := newTestHandler(&stubService{})
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/revise",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Revise(rec, req)
	if status, code := decodeErr(t, rec); status != http.StatusBadRequest || code != "VALIDATION_ERROR" {
		t.Errorf("status=%d code=%q, want 400 VALIDATION_ERROR", status, code)
	}
}

func TestExportHeaders(t *testing.T) {
	html := "<!DOCTYPE html><html><body>Kopi</body></html>"
	stub := &stubService{
		exportProj: &model.Project{ID: "p1", Title: "Website Kopi Senja"},
		exportVer:  &model.Version{ID: "v2", ProjectID: "p1", VersionNumber: 2, HTML: html},
	}
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/export", nil),
		map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Export(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	wantDisp := `attachment; filename="website-kopi-senja-v2.html"`
	if disp := rec.Header().Get("Content-Disposition"); disp != wantDisp {
		t.Errorf("Content-Disposition = %q, want %q", disp, wantDisp)
	}
	if v := rec.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
	if rec.Body.String() != html {
		t.Errorf("export body = %q", rec.Body.String())
	}
}

func TestExportInvalidVersionParam(t *testing.T) {
	h := newTestHandler(&stubService{})
	req := authed(httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/export?version=abc", nil),
		map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	if status, code := decodeErr(t, rec); status != http.StatusBadRequest || code != "VALIDATION_ERROR" {
		t.Errorf("status=%d code=%q, want 400 VALIDATION_ERROR", status, code)
	}
}

func TestDetailNotFoundMapping(t *testing.T) {
	h := newTestHandler(&stubService{getErr: apperror.NotFound("Project not found", nil)})
	req := authed(httptest.NewRequest(http.MethodGet, "/api/v1/projects/other-id", nil),
		map[string]string{"id": "550e8400-e29b-41d4-a716-446655440000"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if status, code := decodeErr(t, rec); status != http.StatusNotFound || code != "NOT_FOUND" {
		t.Errorf("status=%d code=%q, want 404 NOT_FOUND", status, code)
	}
}

func TestUpdateTitleSuccess(t *testing.T) {
	stub := &stubService{updateOut: &model.Project{ID: "p1", Title: "Judul Baru", Status: "draft"}}
	h := newTestHandler(stub)
	req := authed(httptest.NewRequest(http.MethodPatch, "/api/v1/projects/p1",
		strings.NewReader(`{"title":"Judul Baru"}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.UpdateTitle(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data, ok := decodeData(t, rec).(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want object", decodeData(t, rec))
	}
	if data["title"] != "Judul Baru" {
		t.Errorf("title = %v, want Judul Baru", data["title"])
	}
	if _, present := data["latest_version"]; present {
		t.Errorf("update response must not contain latest_version key, got %v", data)
	}
}

func TestUpdateTitleValidation(t *testing.T) {
	h := newTestHandler(&stubService{updateOut: &model.Project{ID: "p1"}})
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{"empty title", `{"title":""}`, "VALIDATION_ERROR"},
		// NOTE: whitespace-only titles pass struct validation here and are
		// rejected after trim in the service (covered in service tests).
		{"missing title", `{}`, "VALIDATION_ERROR"},
		{"too long", `{"title":"` + strings.Repeat("a", 201) + `"}`, "VALIDATION_ERROR"},
		{"unknown field", `{"title":"x","system":"override"}`, "BAD_REQUEST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := authed(httptest.NewRequest(http.MethodPatch, "/api/v1/projects/p1",
				strings.NewReader(tc.body)), map[string]string{"id": "p1"})
			rec := httptest.NewRecorder()
			h.UpdateTitle(rec, req)
			if status, code := decodeErr(t, rec); status != http.StatusBadRequest || code != tc.code {
				t.Errorf("status=%d code=%q, want 400 %s", status, code, tc.code)
			}
		})
	}
}

func TestUpdateTitleNotFoundMapping(t *testing.T) {
	h := newTestHandler(&stubService{updateErr: apperror.NotFound("Project not found", nil)})
	req := authed(httptest.NewRequest(http.MethodPatch, "/api/v1/projects/p1",
		strings.NewReader(`{"title":"x"}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.UpdateTitle(rec, req)
	if status, code := decodeErr(t, rec); status != http.StatusNotFound || code != "NOT_FOUND" {
		t.Errorf("status=%d code=%q, want 404 NOT_FOUND", status, code)
	}
}

func TestUpdateTitleConflictMapping(t *testing.T) {
	h := newTestHandler(&stubService{updateErr: apperror.Conflict("Cannot rename project while generation is in progress", nil)})
	req := authed(httptest.NewRequest(http.MethodPatch, "/api/v1/projects/p1",
		strings.NewReader(`{"title":"x"}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.UpdateTitle(rec, req)
	if status, code := decodeErr(t, rec); status != http.StatusConflict || code != "CONFLICT" {
		t.Errorf("status=%d code=%q, want 409 CONFLICT", status, code)
	}
}

func TestGetAttachmentHeaders(t *testing.T) {
	stub := &stubService{
		attach:    &model.ProjectAttachment{ID: "a1", FileName: "logo.png", FileType: "png", FileSize: 4, MimeType: "image/png"},
		attachBin: "fake",
	}
	h := newTestHandler(stub)
	req := authed(httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/attachments/a1", nil),
		map[string]string{"id": "p1", "attachmentId": "a1"})
	rec := httptest.NewRecorder()
	h.GetAttachment(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}
	if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, "inline") {
		t.Errorf("Content-Disposition = %q, want inline", disp)
	}
	if v := rec.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", v)
	}
}

func TestCreateOmitsLatestVersionKey(t *testing.T) {
	stub := &stubService{createOut: &model.Project{ID: "p1", Title: "T", Status: "draft"}}
	h := newTestHandler(stub)
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects", strings.NewReader(validCreateBody)), nil)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	data, ok := decodeData(t, rec).(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want object", decodeData(t, rec))
	}
	if _, present := data["latest_version"]; present {
		t.Errorf("create response must not contain latest_version key, got %v", data)
	}
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body["data"]
}

// Per api.md, list items carry no latest_version key at all, while the
// detail endpoint returns it as null when the project has no versions.
func TestListOmitsLatestVersionKey(t *testing.T) {
	stub := &stubService{listOut: []*model.Project{
		{ID: "p1", Title: "T", Status: "draft", Brief: model.Brief{Description: "x"}, VersionCount: 0},
	}}
	h := newTestHandler(stub)
	req := authed(httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil), nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items, ok := decodeData(t, rec).([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("data = %v, want 1-item array", decodeData(t, rec))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item = %v, want object", items[0])
	}
	if _, present := item["latest_version"]; present {
		t.Errorf("list item must not contain latest_version key, got %v", item)
	}
}

func TestDetailNullLatestVersion(t *testing.T) {
	stub := &stubService{getProj: &model.Project{ID: "p1", Title: "T", Status: "draft"}}
	h := newTestHandler(stub)
	req := authed(httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1", nil),
		map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data, ok := decodeData(t, rec).(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want object", decodeData(t, rec))
	}
	latest, present := data["latest_version"]
	if !present {
		t.Fatal("detail must contain latest_version key")
	}
	if latest != nil {
		t.Errorf("latest_version = %v, want null for a project without versions", latest)
	}
}

// Generate/revise accept a larger JSON body (AI_MAX_GENERATE_BODY_BYTES,
// 32 MB by default) than other endpoints (AI_MAX_REQUEST_BYTES, 1 MB).
// A ~2 MB body must pass body parsing on generate/revise but be rejected
// as too large on POST /projects.
func TestGenerateBodyLimit(t *testing.T) {
	bigPayload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 1500000)))
	if len(bigPayload) < 1<<20 {
		t.Fatalf("test payload = %d bytes, want > 1 MB", len(bigPayload))
	}

	newHandler := func() *Handler {
		return New(&stubService{beginErr: apperror.AIProvider("AI provider is not configured", nil)},
			testHandlerLogger(),
			config.AIConfig{MaxBytes: 1 << 20, MaxGenerateBodyBytes: 32 << 20},
			config.UploadConfig{MaxSizeBytes: 10 << 20},
			config.ProjectConfig{MaxImages: 4, MaxHTMLBytes: 524288})
	}

	t.Run("generate accepts ~2MB JSON", func(t *testing.T) {
		h := newHandler()
		body := `{"images":[{"mime_type":"image/png","file_name":"big.png","data":"` + bigPayload + `"}]}`
		req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
			strings.NewReader(body)), map[string]string{"id": "p1"})
		rec := httptest.NewRecorder()
		h.Generate(rec, req)
		// 502 from the stubbed service proves the body was parsed and
		// reached the service instead of being rejected as too large.
		if status, code := decodeErr(t, rec); status != http.StatusBadGateway || code != "AI_PROVIDER_ERROR" {
			t.Errorf("status=%d code=%q, want 502 AI_PROVIDER_ERROR", status, code)
		}
	})

	t.Run("revise accepts ~2MB JSON", func(t *testing.T) {
		h := newHandler()
		body := `{"instruction":"ganti warna","images":[{"mime_type":"image/png","file_name":"big.png","data":"` + bigPayload + `"}]}`
		req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/revise",
			strings.NewReader(body)), map[string]string{"id": "p1"})
		rec := httptest.NewRecorder()
		h.Revise(rec, req)
		if status, code := decodeErr(t, rec); status != http.StatusBadGateway || code != "AI_PROVIDER_ERROR" {
			t.Errorf("status=%d code=%q, want 502 AI_PROVIDER_ERROR", status, code)
		}
	})

	t.Run("create rejects ~2MB JSON", func(t *testing.T) {
		h := newHandler()
		body := `{"title":"x","brief":{"description":"` + strings.Repeat("b", 2<<20) + `"}}`
		req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects",
			strings.NewReader(body)), nil)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if status, code := decodeErr(t, rec); status != http.StatusBadRequest || code != "BAD_REQUEST" {
			t.Errorf("status=%d code=%q, want 400 BAD_REQUEST", status, code)
		}
	})
}

func TestSSEHeaders(t *testing.T) {
	stub := &stubService{
		finishVer: &model.Version{ID: "v1", ProjectID: "p1", VersionNumber: 1, Kind: "generate", HTML: "<!DOCTYPE html><html></html>"},
	}
	stub.beginPending = pendingFor("p1", scriptedStream([]string{"<!DOCTYPE html>"}, false))
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)

	want := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestSSEHeartbeat(t *testing.T) {
	oldInterval := sseHeartbeatInterval
	sseHeartbeatInterval = 20 * time.Millisecond
	defer func() { sseHeartbeatInterval = oldInterval }()

	// Stream stays silent long enough for several heartbeats, then finishes.
	ch := make(chan aiprovider.StreamChunk, 1)
	go func() {
		time.Sleep(120 * time.Millisecond)
		ch <- aiprovider.StreamChunk{Done: true}
		close(ch)
	}()
	stub := &stubService{
		finishVer: &model.Version{ID: "v1", ProjectID: "p1", VersionNumber: 1, Kind: "generate", HTML: "<!DOCTYPE html><html></html>"},
	}
	stub.beginPending = pendingFor("p1", ch)
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)

	body := rec.Body.String()
	if got := strings.Count(body, ": ping"); got < 2 {
		t.Errorf("heartbeat comments = %d, want >= 2\n%s", got, body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("stream must still end with done:\n%s", body)
	}
}

func TestSSEMidStreamErrorEnvelope(t *testing.T) {
	stub := &stubService{}
	stub.beginPending = pendingFor("p1", scriptedStream([]string{"<partial"}, true))
	h := newTestHandler(stub)

	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/generate",
		strings.NewReader(`{}`)), map[string]string{"id": "p1"})
	rec := httptest.NewRecorder()
	h.Generate(rec, req)

	body := rec.Body.String()
	lines := strings.Split(body, "\n")
	var dataLine string
	for i, line := range lines {
		if line == "event: error" && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "data: ") {
			dataLine = strings.TrimPrefix(lines[i+1], "data: ")
		}
	}
	if dataLine == "" {
		t.Fatalf("no error event data found:\n%s", body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
		t.Fatalf("error data is not JSON: %v", err)
	}
	if payload["success"] != false {
		t.Errorf("envelope success = %v, want false", payload["success"])
	}
	errObj, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope error = %v, want object", payload["error"])
	}
	if errObj["code"] != "AI_PROVIDER_ERROR" {
		t.Errorf("error code = %v, want AI_PROVIDER_ERROR", errObj["code"])
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Errorf("error message = %v, want string", errObj["message"])
	}
}

// TestStreamSurvivesShortServerWriteTimeout proves the per-write deadline
// refresh actually extends the stream past http.Server.WriteTimeout: a real
// httptest server with a 200ms total write timeout serves a ~1s heartbeat
// stream through the same production middleware chain (RequestID,
// RequestLogger, Recovery, CORS, SecurityHeaders). If the wrapped writer
// chain broke ResponseController/Unwrap, the stream would be cut at 200ms.
func TestStreamSurvivesShortServerWriteTimeout(t *testing.T) {
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := newSSEWriter(w)
		sw.heartbeat = 50 * time.Millisecond
		sw.writeTimeout = 5 * time.Second
		ticker := time.NewTicker(sw.heartbeat)
		defer ticker.Stop()
		end := time.After(time.Second)
		for {
			select {
			case <-ticker.C:
				sw.writeHeartbeat()
			case <-end:
				sw.writeEvent("done", map[string]any{"ok": true})
				return
			}
		}
	})

	var h http.Handler = stream
	h = middleware.SecurityHeaders(h)
	h = middleware.CORS([]string{"http://localhost:3000"})(h)
	h = middleware.Recovery(testHandlerLogger())(h)
	h = middleware.RequestLogger(testHandlerLogger())(h)
	h = middleware.RequestID(h)

	srv := httptest.NewUnstartedServer(h)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	start := time.Now()
	resp, err := http.Get(srv.URL) //nolint:noctx,gosec
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Errorf("stream lasted %v, want ~1s (server WriteTimeout is 200ms)", elapsed)
	}
	if got := strings.Count(string(body), ": ping"); got < 10 {
		t.Errorf("heartbeats = %d, want >= 10 (stream was cut early):\n%s", got, body)
	}
	if !strings.Contains(string(body), "event: done") {
		t.Errorf("stream missing terminal done event:\n%s", body)
	}
}
