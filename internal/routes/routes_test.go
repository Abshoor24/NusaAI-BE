package routes

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Abshoor24/backend-go/internal/apperror"
	"github.com/Abshoor24/backend-go/internal/config"
	"github.com/Abshoor24/backend-go/internal/jwt"
	"github.com/Abshoor24/backend-go/internal/model"
	aiprovider "github.com/Abshoor24/backend-go/internal/modules/ai/provider"
	projectmodel "github.com/Abshoor24/backend-go/internal/modules/project/model"
	projectservice "github.com/Abshoor24/backend-go/internal/modules/project/service"
)

type dummyAuthService struct{}

func (d *dummyAuthService) Register(_ context.Context, _ model.AuthRegisterRequest) (*model.AuthResponse, error) {
	return nil, nil
}
func (d *dummyAuthService) Login(_ context.Context, _ model.AuthLoginRequest) (*model.AuthResponse, error) {
	return nil, nil
}
func (d *dummyAuthService) Me(_ context.Context, _ string) (*model.UserResponse, error) {
	return nil, nil
}

// dummyProjectService returns canned data so route wiring (auth, rate limit,
// SSE headers) can be tested without a database or an LLM provider.
type dummyProjectService struct{}

func (d *dummyProjectService) Create(_ context.Context, _ string, req projectmodel.CreateProjectRequest) (*projectmodel.Project, error) {
	return &projectmodel.Project{
		ID: "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01", Title: "T", Status: projectmodel.StatusDraft,
		Brief: req.Brief, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}
func (d *dummyProjectService) List(_ context.Context, _ string, _, _ int) ([]*projectmodel.Project, error) {
	return []*projectmodel.Project{}, nil
}
func (d *dummyProjectService) Get(_ context.Context, _, _ string) (*projectmodel.Project, *projectmodel.Version, error) {
	return &projectmodel.Project{ID: "p", Title: "T", Status: projectmodel.StatusDraft}, nil, nil
}
func (d *dummyProjectService) Delete(_ context.Context, _, _ string) error { return nil }
func (d *dummyProjectService) UpdateTitle(_ context.Context, _, _ string, title string) (*projectmodel.Project, error) {
	return &projectmodel.Project{ID: "p", Title: title, Status: projectmodel.StatusDraft}, nil
}
func (d *dummyProjectService) ListVersions(_ context.Context, _, _ string) ([]*projectmodel.Version, error) {
	return []*projectmodel.Version{}, nil
}
func (d *dummyProjectService) GetVersion(_ context.Context, _, _ string, _ int) (*projectmodel.Version, error) {
	return nil, apperror.NotFound("Version not found", nil)
}
func (d *dummyProjectService) ExportTarget(_ context.Context, _, _ string, _ *int) (*projectmodel.Project, *projectmodel.Version, error) {
	return &projectmodel.Project{ID: "p", Title: "T"},
		&projectmodel.Version{ID: "v", ProjectID: "p", VersionNumber: 1, HTML: "<!DOCTYPE html><html></html>"}, nil
}
func (d *dummyProjectService) ListAttachments(_ context.Context, _, _ string) ([]*projectmodel.ProjectAttachment, error) {
	return []*projectmodel.ProjectAttachment{}, nil
}
func (d *dummyProjectService) GetAttachment(_ context.Context, _, _, _ string) (*projectmodel.ProjectAttachment, io.ReadCloser, error) {
	return &projectmodel.ProjectAttachment{FileName: "a.png", FileType: "png", FileSize: 3, MimeType: "image/png"},
		io.NopCloser(strings.NewReader("bin")), nil
}
func (d *dummyProjectService) pending() *projectservice.PendingStream {
	ch := make(chan aiprovider.StreamChunk, 1)
	ch <- aiprovider.StreamChunk{Done: true}
	close(ch)
	return &projectservice.PendingStream{
		Project:   &projectmodel.Project{ID: "p", Status: projectmodel.StatusGenerating},
		Kind:      projectmodel.KindGenerate,
		Stream:    ch,
		StartedAt: time.Now(),
		MaxBytes:  1 << 20,
	}
}
func (d *dummyProjectService) BeginGenerate(_ context.Context, _ string, _ string, _ []projectmodel.ImagePayload) (*projectservice.PendingStream, error) {
	return d.pending(), nil
}
func (d *dummyProjectService) BeginRevise(_ context.Context, _ string, _ string, _ string, _ *int, _ []projectmodel.ImagePayload) (*projectservice.PendingStream, error) {
	return d.pending(), nil
}
func (d *dummyProjectService) FinishStream(_ context.Context, _ *projectservice.PendingStream, _ string) (*projectmodel.Version, error) {
	return nil, apperror.AIProvider("Generated output is not a valid HTML document", nil)
}
func (d *dummyProjectService) FailStream(_ context.Context, _ *projectservice.PendingStream) {}

var _ projectservice.Service = (*dummyProjectService)(nil)

func setupTestRouter() (http.Handler, *jwt.Manager) {
	return setupTestRouterWithRate(config.RateLimitConfig{Enabled: false}, config.RateLimitConfig{Enabled: false})
}

func setupTestRouterWithCORS(origins []string) (http.Handler, *jwt.Manager) {
	return setupTestRouterFull(
		config.RateLimitConfig{Enabled: false},
		config.RateLimitConfig{Enabled: false},
		origins,
	)
}

func setupTestRouterWithRate(general, generate config.RateLimitConfig) (http.Handler, *jwt.Manager) {
	return setupTestRouterFull(general, generate, []string{"*"})
}

func setupTestRouterFull(general, generate config.RateLimitConfig, origins []string) (http.Handler, *jwt.Manager) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: "8080"},
		CORS:   config.CORSConfig{AllowedOrigins: origins},
		JWT: config.JWTConfig{
			Secret:     "secret-must-be-long-enough-for-testing",
			Expiration: time.Hour,
			Issuer:     "test",
		},
		AI:       config.AIConfig{MaxBytes: 1 << 20},
		Rate:     general,
		Generate: generate,
		Upload:   config.UploadConfig{Dir: "uploads", MaxSizeBytes: 10 << 20},
		Project:  config.ProjectConfig{MaxImages: 4, MaxHTMLBytes: 1 << 20},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := jwt.NewManager(cfg.JWT)

	router := New(Dependencies{
		Config:  cfg,
		Logger:  logger,
		DB:      nil,
		Auth:    &dummyAuthService{},
		AI:      nil, // /ai/health only reads config, never the service
		Project: &dummyProjectService{},
		Tokens:  tokens,
	})
	return router, tokens
}

func testToken(t *testing.T, tokens *jwt.Manager) string {
	t.Helper()
	tokenStr, _, err := tokens.Generate(&model.User{ID: "user-1", Email: "test@example.com"})
	if err != nil {
		t.Fatalf("token generate failed: %v", err)
	}
	return tokenStr
}

// Deferred modules must be unreachable: the old AI chat/conversation,
// document, attachment, and execution routes return the router 404.
func TestRemovedEndpointsReturn404(t *testing.T) {
	router, tokens := setupTestRouter()
	tokenStr := testToken(t, tokens)

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/ai/chat", `{"message":"Hello"}`},
		{http.MethodPost, "/api/v1/ai/chat/stream", `{"message":"Hello"}`},
		{http.MethodGet, "/api/v1/ai/conversations", ""},
		{http.MethodGet, "/api/v1/ai/conversations/conv-1", ""},
		{http.MethodDelete, "/api/v1/ai/conversations/conv-1", ""},
		{http.MethodGet, "/api/v1/ai/attachments/att-1", ""},
		{http.MethodGet, "/api/v1/attachments/att-1", ""},
		{http.MethodPost, "/api/v1/ai/documents", `{"title":"Doc","content":"x"}`},
		{http.MethodPost, "/api/v1/ai/documents/upload", ""},
		{http.MethodGet, "/api/v1/ai/documents", ""},
		{http.MethodGet, "/api/v1/ai/documents/doc-1", ""},
		{http.MethodGet, "/api/v1/ai/documents/doc-1/status", ""},
		{http.MethodDelete, "/api/v1/ai/documents/doc-1", ""},
		{http.MethodPost, "/api/v1/documents/upload", ""},
		{http.MethodGet, "/api/v1/documents/doc-1/status", ""},
		{http.MethodPost, "/api/v1/execute", `{"language":"python","code":"print(1)"}`},
		{http.MethodGet, "/api/v1/execute/history", ""},
		{http.MethodGet, "/api/v1/execute/exec-1", ""},
	}

	for _, ep := range endpoints {
		for _, withAuth := range []bool{false, true} {
			var body io.Reader
			if ep.body != "" {
				body = strings.NewReader(ep.body)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(ep.method, ep.path, body)
			if withAuth {
				req.Header.Set("Authorization", "Bearer "+tokenStr)
			}
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s (auth=%v): got %d, want 404", ep.method, ep.path, withAuth, rec.Code)
			}
		}
	}
}

func TestProjectEndpointsRequireAuth(t *testing.T) {
	router, _ := setupTestRouter()

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/projects", `{"brief":{"description":"Landing page untuk kedai kopi."}}`},
		{http.MethodGet, "/api/v1/projects", ""},
		{http.MethodGet, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01", ""},
		{http.MethodDelete, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01", ""},
		{http.MethodPatch, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01", `{"title":"x"}`},
		{http.MethodGet, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/versions", ""},
		{http.MethodGet, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/versions/1", ""},
		{http.MethodGet, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/export", ""},
		{http.MethodGet, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/attachments", ""},
		{http.MethodGet, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/attachments/a1", ""},
		{http.MethodPost, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/generate", `{}`},
		{http.MethodPost, "/api/v1/projects/5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01/revise", `{"instruction":"ubah"}`},
	}

	for _, ep := range endpoints {
		var body io.Reader
		if ep.body != "" {
			body = strings.NewReader(ep.body)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(ep.method, ep.path, body)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: got %d, want 401", ep.method, ep.path, rec.Code)
		}
	}
}

func TestProjectCRUDRoutesAuthorized(t *testing.T) {
	router, tokens := setupTestRouter()
	tokenStr := testToken(t, tokens)
	id := "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01"

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, r)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost, "/api/v1/projects", `{"brief":{"description":"Landing page untuk kedai kopi lokal."}}`); rec.Code != http.StatusCreated {
		t.Errorf("POST /projects: got %d, want 201", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/projects", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /projects: got %d, want 200", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/projects/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("GET /projects/{id}: got %d, want 200", rec.Code)
	}
	if rec := do(http.MethodDelete, "/api/v1/projects/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("DELETE /projects/{id}: got %d, want 200", rec.Code)
	}
	if rec := do(http.MethodPatch, "/api/v1/projects/"+id, `{"title":"Judul Baru"}`); rec.Code != http.StatusOK {
		t.Errorf("PATCH /projects/{id}: got %d, want 200", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/projects/"+id+"/versions", ""); rec.Code != http.StatusOK {
		t.Errorf("GET versions: got %d, want 200", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/projects/"+id+"/export", ""); rec.Code != http.StatusOK {
		t.Errorf("GET export: got %d, want 200", rec.Code)
	} else if disp := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "attachment;") {
		t.Errorf("export disposition = %q, want attachment", disp)
	}
	if rec := do(http.MethodGet, "/api/v1/projects/"+id+"/attachments", ""); rec.Code != http.StatusOK {
		t.Errorf("GET attachments: got %d, want 200", rec.Code)
	}
}

func TestGenerateSSEViaHeaderAndQueryToken(t *testing.T) {
	router, tokens := setupTestRouter()
	tokenStr := testToken(t, tokens)
	id := "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01"

	// Authorization header.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+id+"/generate", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generate with header token: got %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// ?token= query param (EventSource clients).
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+id+"/revise?token="+tokenStr, strings.NewReader(`{"instruction":"ubah warna"}`))
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("revise with query token: got %d, want 200", rec2.Code)
	}
	if ct := rec2.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestGenerateRateLimitTier(t *testing.T) {
	router, tokens := setupTestRouterWithRate(
		config.RateLimitConfig{Enabled: false},
		config.RateLimitConfig{Enabled: true, PerMinute: 1, Burst: 1},
	)
	tokenStr := testToken(t, tokens)
	id := "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01"

	do := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+id+"/generate", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(); rec.Code != http.StatusOK {
		t.Fatalf("first generate: got %d, want 200", rec.Code)
	}
	rec := do()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second generate: got %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
}
func TestPublicHealthDoesNotRequireAuth(t *testing.T) {
	router, _ := setupTestRouter()

	for _, path := range []string{"/health", "/api/v1/ai/health"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: got %d, want 200", path, rec.Code)
		}
	}
}

func assertJSONEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d", rec.Code, wantStatus)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body must be JSON: %v", err)
	}
	if body["success"] != false {
		t.Errorf("envelope success = %v, want false", body["success"])
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope error = %v, want object", body["error"])
	}
	if errObj["code"] != wantCode {
		t.Errorf("error code = %v, want %s", errObj["code"], wantCode)
	}
}

func TestUnknownRouteReturnsJSONEnvelope(t *testing.T) {
	router, _ := setupTestRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)
	router.ServeHTTP(rec, req)
	assertJSONEnvelope(t, rec, http.StatusNotFound, "NOT_FOUND")
}

func TestWrongMethodReturnsJSONEnvelope(t *testing.T) {
	router, tokens := setupTestRouter()
	tokenStr := testToken(t, tokens)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	router.ServeHTTP(rec, req)
	assertJSONEnvelope(t, rec, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
}

func TestUnauthorizedCarriesCORSHeaders(t *testing.T) {
	router, _ := setupTestRouterWithCORS([]string{"http://localhost:3000"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin = %q on 401, want echoed origin", got)
	}
	assertJSONEnvelope(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestStreamCarriesCORSHeaders(t *testing.T) {
	router, tokens := setupTestRouterWithCORS([]string{"http://localhost:3000"})
	tokenStr := testToken(t, tokens)
	id := "5e2b7a10-9c34-4f0e-8d55-2a1c6b7e9f01"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+id+"/generate", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("Origin", "http://localhost:3000")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin = %q on stream, want echoed origin", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q on stream, want Origin", got)
	}
}

func TestOversizedBodyReturnsJSONEnvelope(t *testing.T) {
	router, tokens := setupTestRouter()
	tokenStr := testToken(t, tokens)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		strings.NewReader(`{"title":"x","brief":{"description":"`+strings.Repeat("b", 2<<20)+`"}}`))
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	router.ServeHTTP(rec, req)
	assertJSONEnvelope(t, rec, http.StatusBadRequest, "BAD_REQUEST")
}
