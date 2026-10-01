package service

import (
	"strings"
	"testing"
)

func TestCleanHTML(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "  " + testHTML + "\n", testHTML},
		{"fenced html", "```html\n" + testHTML + "\n```", testHTML},
		{"fenced bare", "```\n" + testHTML + "\n```", testHTML},
		{"fenced trailing text", "```html\n" + testHTML + "\n```\nDone!", testHTML},
	}
	for _, tc := range cases {
		if got := CleanHTML(tc.raw); got != tc.want {
			t.Errorf("%s: CleanHTML = %q", tc.name, got)
		}
	}
}

func TestCleanAndValidate(t *testing.T) {
	const maxBytes = 1024
	tooBig := "<!DOCTYPE html><html>" + strings.Repeat("x", 2048) + "</html>"
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid doctype", testHTML, false},
		{"valid fenced", "```html\n" + testHTML + "\n```", false},
		{"uppercase doctype", "<!DOCTYPE HTML><HTML><BODY>x</BODY></HTML>", false},
		{"html tag only", "<html><body>x</body></html>", false},
		{"not html", "Maaf, saya tidak bisa membantu.", true},
		{"missing close", "<!DOCTYPE html><html><body>x", true},
		{"empty", "", true},
		{"too big", tooBig, true},
		{"preamble before fence", "Berikut hasilnya:\n```html\n" + testHTML + "\n```", false},
		{"preamble before bare doctype", "Ini websitenya:\n" + testHTML, false},
		{"trailing text after close", testHTML + "\nSemoga membantu!", false},
		{"fence without doctype", "```html\n<p>saja</p>\n```", true},
	}
	for _, tc := range cases {
		_, err := CleanAndValidate(tc.raw, maxBytes)
		if tc.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

func TestExtractHTML(t *testing.T) {
	doc := "<!DOCTYPE html><html><body>x</body></html>"
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "  " + doc + "\n", doc},
		{"strips preamble", "Tentu, ini hasilnya:\n" + doc, doc},
		{"strips fence and preamble", "Lihat:\n```html\n" + doc + "\n```", doc},
		{"strips trailing text", doc + "\nSelesai!", doc},
		{"uppercase markers", "<!DOCTYPE HTML><HTML><BODY>x</BODY></HTML>", "<!DOCTYPE HTML><HTML><BODY>x</BODY></HTML>"},
		{"truncated has no close", "<!DOCTYPE html><html><body>x", ""},
		{"no markers at all", "halo dunia", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		if got := ExtractHTML(tc.raw); got != tc.want {
			t.Errorf("%s: ExtractHTML = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOutputEdges(t *testing.T) {
	if got := headEdge("abc"); got != "abc" {
		t.Errorf("headEdge short = %q", got)
	}
	if got := tailEdge("abc"); got != "abc" {
		t.Errorf("tailEdge short = %q", got)
	}
	long := strings.Repeat("x", 500)
	if got := headEdge(long); len(got) != 200 {
		t.Errorf("headEdge len = %d, want 200", len(got))
	}
	if got := tailEdge(long); len(got) != 200 {
		t.Errorf("tailEdge len = %d, want 200", len(got))
	}
}
