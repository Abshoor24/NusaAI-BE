package examples

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newTestRouter() http.Handler {
	r := chi.NewRouter()
	h := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.Get("/examples", h.List)
	r.Get("/examples/{slug}", h.HTML)
	return r
}

func TestHandlerListUsesEnvelopeAndLabel(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/examples", nil)
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Success bool   `json:"success"`
		Data    []Site `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || len(body.Data) != 5 {
		t.Fatalf("unexpected body: %+v", body)
	}
	for _, site := range body.Data {
		if site.Label != Label {
			t.Fatalf("site %q label = %q", site.Slug, site.Label)
		}
	}
}

func TestHandlerHTMLReturnsRawDocument(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/examples/toko", nil)
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
	html := rec.Body.String()
	if !strings.Contains(html, "<title>Toko Melati") {
		t.Fatalf("unexpected HTML: %q", html[:min(len(html), 120)])
	}
	if !strings.Contains(html, ">Contoh hasil</div>") {
		t.Fatal("HTML is missing the Contoh hasil label")
	}
}

func TestHandlerHTMLUnknownSlugReturnsJSON404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/examples/tidak-ada", nil)
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q", got)
	}
}
