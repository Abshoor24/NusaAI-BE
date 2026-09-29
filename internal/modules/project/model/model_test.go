package model

import (
	"strings"
	"testing"
)

func validBrief() Brief {
	return Brief{
		Description:  "Landing page untuk kedai kopi lokal di Purwokerto dengan menu dan jam buka.",
		WebsiteType:  "landing_page",
		BusinessName: "Kopi Senja",
		Style:        "hangat, rustic, modern",
		ColorPalette: "cokelat kopi dan krem",
		Language:     "id",
		Sections:     []string{"hero", "menu", "about"},
	}
}

func TestBriefValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Brief)
		wantErr bool
		field   string
	}{
		{"valid", func(b *Brief) {}, false, ""},
		{"description too short", func(b *Brief) { b.Description = "pendek" }, true, "description"},
		{"description empty", func(b *Brief) { b.Description = "" }, true, "description"},
		{"description too long", func(b *Brief) { b.Description = strings.Repeat("a", 4001) }, true, "description"},
		{"bad website type", func(b *Brief) { b.WebsiteType = "mobile_app" }, true, "website_type"},
		{"bad language", func(b *Brief) { b.Language = "fr" }, true, "language"},
		{"business name too long", func(b *Brief) { b.BusinessName = strings.Repeat("a", 121) }, true, "business_name"},
		{"style too long", func(b *Brief) { b.Style = strings.Repeat("a", 201) }, true, "style"},
		{"color too long", func(b *Brief) { b.ColorPalette = strings.Repeat("a", 201) }, true, "color_palette"},
		{"too many sections", func(b *Brief) {
			b.Sections = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"}
		}, true, "sections"},
		{"section item too long", func(b *Brief) { b.Sections = []string{strings.Repeat("a", 51)} }, true, "sections"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := validBrief()
			tc.mutate(&b)
			b.ApplyDefaults()
			fields := Validate(CreateProjectRequest{Brief: b})
			if !tc.wantErr && fields != nil {
				t.Fatalf("expected valid, got fields %v", fields)
			}
			if tc.wantErr {
				if fields == nil {
					t.Fatalf("expected validation error on %q, got nil", tc.field)
				}
				found := false
				for k := range fields {
					if strings.Contains(k, tc.field) {
						found = true
					}
				}
				if !found {
					t.Errorf("expected error on field containing %q, got %v", tc.field, fields)
				}
			}
		})
	}
}

func TestBriefDefaults(t *testing.T) {
	b := Brief{Description: "Deskripsi yang cukup panjang untuk lolos validasi minimal."}
	b.ApplyDefaults()
	if b.WebsiteType != "landing_page" {
		t.Errorf("website_type default = %q, want landing_page", b.WebsiteType)
	}
	if b.Language != "id" {
		t.Errorf("language default = %q, want id", b.Language)
	}

	keep := Brief{Description: b.Description, WebsiteType: "blog", Language: "en"}
	keep.ApplyDefaults()
	if keep.WebsiteType != "blog" || keep.Language != "en" {
		t.Errorf("ApplyDefaults overwrote explicit values: %+v", keep)
	}
}

func TestDeriveTitle(t *testing.T) {
	if got := DeriveTitle("Judul Eksplisit", validBrief()); got != "Judul Eksplisit" {
		t.Errorf("explicit title = %q", got)
	}
	b := validBrief()
	if got := DeriveTitle("", b); got != "Kopi Senja" {
		t.Errorf("business_name fallback = %q, want Kopi Senja", got)
	}
	b.BusinessName = ""
	if got := DeriveTitle("  ", b); got != strings.TrimSpace(b.Description)[:60] {
		t.Errorf("description fallback = %q", got)
	}
	long := Brief{Description: strings.Repeat("x", 100)}
	if got := DeriveTitle("", long); len([]rune(got)) != 60 {
		t.Errorf("long description fallback runes = %d, want 60", len([]rune(got)))
	}
}

func TestCreateProjectTitleLimit(t *testing.T) {
	req := CreateProjectRequest{Title: strings.Repeat("a", 201), Brief: validBrief()}
	if fields := Validate(req); fields == nil {
		t.Error("expected validation error for title > 200 chars")
	}
}

func TestReviseValidation(t *testing.T) {
	if fields := Validate(ReviseRequest{}); fields == nil {
		t.Error("expected validation error for empty instruction")
	}
	n := 0
	if fields := Validate(ReviseRequest{Instruction: "ubah warna", BaseVersion: &n}); fields == nil {
		t.Error("expected validation error for base_version < 1")
	}
	if fields := Validate(ReviseRequest{Instruction: strings.Repeat("a", 4001)}); fields == nil {
		t.Error("expected validation error for instruction > 4000 chars")
	}
}
