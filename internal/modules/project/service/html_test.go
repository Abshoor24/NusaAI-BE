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
