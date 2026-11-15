package main

import (
	"strings"
	"testing"
)

func TestBriefFixturesUseFormatBriefShape(t *testing.T) {
	briefs, err := loadBriefs("")
	if err != nil {
		t.Fatalf("loadBriefs: %v", err)
	}
	if len(briefs) != len(requiredBriefs) {
		t.Fatalf("brief count = %d, want %d", len(briefs), len(requiredBriefs))
	}
	for _, brief := range briefs {
		if !strings.HasPrefix(brief.Text, "Description: ") {
			t.Errorf("%s: missing Description line", brief.Name)
		}
		if !strings.Contains(brief.Text, "\nWebsite type: ") {
			t.Errorf("%s: missing Website type line", brief.Name)
		}
		if !strings.Contains(brief.Text, "\nContent language: ") {
			t.Errorf("%s: missing Content language line", brief.Name)
		}
		if len(briefDigest(brief.Text)) != 8 {
			t.Errorf("%s: digest is not 8 characters", brief.Name)
		}
	}
}

func TestBriefFixturesContainExpectedFacts(t *testing.T) {
	briefs, err := loadBriefs("")
	if err != nil {
		t.Fatalf("loadBriefs: %v", err)
	}
	byName := make(map[string]Brief, len(briefs))
	for _, brief := range briefs {
		byName[brief.Name] = brief
	}

	full := byName["full"]
	for _, want := range []string{"WhatsApp:", "Jam buka:", "Menu:", "Color palette:", "Rp15.000"} {
		if !strings.Contains(full.Text, want) {
			t.Errorf("full: missing %q", want)
		}
	}
	if len(full.Facts.Emails) != 1 || full.Facts.WhatsApp == "" || !full.Facts.HasPalette {
		t.Errorf("full facts incomplete: %+v", full.Facts)
	}

	service := byName["service"]
	for _, want := range []string{"WhatsApp:", "Jalan Rekaan", "Jam buka:", "Layanan:"} {
		if !strings.Contains(service.Text, want) {
			t.Errorf("service: missing %q", want)
		}
	}
	if strings.Contains(service.Text, "Rp") {
		t.Error("service: should not contain prices")
	}

	waOnly := byName["wa_only"]
	if !strings.Contains(waOnly.Text, "WhatsApp:") {
		t.Error("wa_only: missing WhatsApp")
	}
	for _, unwanted := range []string{"Jam buka:", "Menu:", "Jalan "} {
		if strings.Contains(waOnly.Text, unwanted) {
			t.Errorf("wa_only: must not contain %q", unwanted)
		}
	}

	minimal := byName["minimal"]
	for _, unwanted := range []string{"WhatsApp", "Email", "Jam buka", "Menu", "Color palette", "Rp"} {
		if strings.Contains(minimal.Text, unwanted) {
			t.Errorf("minimal: must not contain %q", unwanted)
		}
	}
	if minimal.Facts.HasContact || minimal.Facts.HasHours {
		t.Errorf("minimal facts should have no contact/hours: %+v", minimal.Facts)
	}

	nopalette := byName["nopalette"]
	if strings.Contains(nopalette.Text, "Color palette:") || nopalette.Facts.HasPalette {
		t.Error("nopalette: must not contain a palette")
	}
	if !allowedPalette(nopalette.Facts)["#e0a93b"] {
		t.Error("nopalette: V8-4 fallback tokens should be allowed")
	}

	if strings.Contains(full.Text, "Kopi Arum Fiktif") && !strings.Contains(minimal.Text, "Kedai Roti Fiktif") {
		t.Error("fixtures should be distinct fictional businesses")
	}
}

func TestSelectBriefsRejectsUnknownAndNeverFallsBack(t *testing.T) {
	briefs, err := loadBriefs("")
	if err != nil {
		t.Fatalf("loadBriefs: %v", err)
	}
	selected, err := selectBriefs(briefs, "unknown")
	if err == nil {
		t.Fatal("unknown brief: want error, got nil")
	}
	if selected != nil {
		t.Fatalf("unknown brief selected %v, want nil", availableBriefNames(selected))
	}
	for _, name := range requiredBriefs {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not list available brief %q", err, name)
		}
	}

	selected, err = selectBriefs(briefs, "minimal")
	if err != nil || len(selected) != 1 || selected[0].Name != "minimal" {
		t.Fatalf("select minimal = %v, %v", availableBriefNames(selected), err)
	}
}

func TestMinimalBriefIsNotFullBrief(t *testing.T) {
	briefs, err := loadBriefs("")
	if err != nil {
		t.Fatalf("loadBriefs: %v", err)
	}
	selected, err := selectBriefs(briefs, "minimal")
	if err != nil {
		t.Fatalf("select minimal: %v", err)
	}
	full := briefs[0]
	if selected[0].Text == full.Text {
		t.Fatal("minimal brief must not be the full brief")
	}
	if selected[0].Facts.HasHours {
		t.Fatal("minimal brief unexpectedly has hours")
	}
}
