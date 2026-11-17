package examples

import (
	"bytes"
	"strings"
	"testing"
)

func TestListHasFiveLabeledExamples(t *testing.T) {
	sites := List()
	if len(sites) != 5 {
		t.Fatalf("expected 5 examples, got %d", len(sites))
	}

	seen := make(map[string]bool, len(sites))
	for _, site := range sites {
		if site.Slug == "" || site.Category == "" || site.Title == "" || site.Description == "" {
			t.Fatalf("incomplete example metadata: %+v", site)
		}
		if site.Label != Label {
			t.Fatalf("example %q label = %q, want %q", site.Slug, site.Label, Label)
		}
		if seen[site.Slug] {
			t.Fatalf("duplicate slug %q", site.Slug)
		}
		seen[site.Slug] = true
	}

	for _, want := range []string{"kuliner", "jasa", "toko", "portofolio", "acara"} {
		if !seen[want] {
			t.Fatalf("missing example %q", want)
		}
	}
}

func TestGetReturnsLabeledHTML(t *testing.T) {
	for _, site := range List() {
		got, html, ok := Get(site.Slug)
		if !ok {
			t.Fatalf("Get(%q) not found", site.Slug)
		}
		if got.Slug != site.Slug {
			t.Fatalf("Get(%q) returned %q", site.Slug, got.Slug)
		}
		if len(html) == 0 {
			t.Fatalf("Get(%q) returned empty HTML", site.Slug)
		}
		if !bytes.Contains(html, []byte("<!DOCTYPE html>")) {
			t.Fatalf("Get(%q) is not a complete HTML document", site.Slug)
		}
		if !bytes.Contains(html, []byte(">Contoh hasil</div>")) {
			t.Fatalf("Get(%q) is missing the visible Contoh hasil label", site.Slug)
		}
	}
}

func TestGetRejectsUnknownAndTraversalSlugs(t *testing.T) {
	for _, slug := range []string{"", "unknown", "../sites/toko", "toko.html"} {
		if _, _, ok := Get(slug); ok {
			t.Fatalf("Get(%q) unexpectedly succeeded", slug)
		}
	}
}

func TestListReturnsCopy(t *testing.T) {
	sites := List()
	sites[0].Title = "changed"
	if strings.EqualFold(List()[0].Title, "changed") {
		t.Fatal("List must not expose the package catalog")
	}
}
