package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cleanLintHTML = `<!DOCTYPE html>
<html lang="id">
<head>
<!--
DESIGN PLAN: A quiet typographic concept for a fictional local business. The signature widget is a compact fact strip. The hero is asymmetric with a strong headline. Palette uses only brief tokens. Sections run hero, facts, about, menu, contact.
FACTS NAME: Fiktif EMAIL: none PRIMARY: NONE ACCENT: NONE
-->
<script src="https://cdn.tailwindcss.com"></script>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Fraunces&display=swap">
</head>
<body class="bg-paper text-ink">
<section class="bg-ink"><p class="text-accent">Fiktif</p></section>
<section class="bg-paper"><p>Fakta singkat dari brief.</p></section>
<section class="bg-tint"><p>Tentang usaha fiktif ini.</p></section>
<section class="bg-paper"><p>Informasi yang tersedia di brief.</p></section>
<section class="bg-ink"><p class="text-accent">Penutup</p></section>
</body>
</html>
`

func lintMap(t *testing.T, brief Brief, doc string) map[string]lintFinding {
	t.Helper()
	findings := lintHTML(doc, brief)
	out := make(map[string]lintFinding, len(findings))
	for _, finding := range findings {
		out[finding.Rule] = finding
	}
	return out
}

func loadBriefByName(t *testing.T, name string) Brief {
	t.Helper()
	briefs, err := loadBriefs("")
	if err != nil {
		t.Fatalf("loadBriefs: %v", err)
	}
	selected, err := selectBriefs(briefs, name)
	if err != nil {
		t.Fatalf("selectBriefs(%s): %v", name, err)
	}
	return selected[0]
}

func TestLintCleanHTMLPassesAllRules(t *testing.T) {
	full := loadBriefByName(t, "full")
	for rule, finding := range lintMap(t, full, cleanLintHTML) {
		if finding.Status == lintFail {
			t.Errorf("clean HTML failed %s: %s", rule, finding.Evidence)
		}
	}
}

func TestLintFailRulesHavePositiveAndNegativeCases(t *testing.T) {
	cases := []struct {
		name     string
		rule     string
		brief    string
		mutate   func(string) string
		wantFail bool
	}{
		{
			name: "email", rule: ruleEmail, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<a href="mailto:bad@example.test">email</a></body>`, 1)
			},
		},
		{
			name: "phone", rule: rulePhone, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<a href="https://wa.me/6289999999999">chat</a></body>`, 1)
			},
		},
		{
			name: "hours", rule: ruleHours, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<p>Senin 10.00-22.00</p></body>`, 1)
			},
		},
		{
			name: "forbidden", rule: ruleForbidden, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<p>Kopi terbaik di kota.</p></body>`, 1)
			},
		},
		{
			name: "permanent-open", rule: ruleOpenStatus, brief: "minimal", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<span class="pill">Buka Sekarang</span></body>`, 1)
			},
		},
		{
			name: "contact-cta", rule: ruleContactCTA, brief: "minimal", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<a href="#kontak">Hubungi Kami</a></body>`, 1)
			},
		},
		{
			name: "design-plan-no-facts", rule: ruleDesignPlan, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "FACTS NAME:", "NOTES NAME:", 1)
			},
		},
		{
			name: "italic", rule: ruleItalic, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<em>halo</em></body>`, 1)
			},
		},
		{
			name: "external-script", rule: ruleExternal, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<script src="https://evil.test/app.js"></script></body>`, 1)
			},
		},
		{
			name: "palette", rule: rulePalette, brief: "full", wantFail: true,
			mutate: func(s string) string {
				return strings.Replace(s, "</body>", `<div class="bg-[#123456]">x</div></body>`, 1)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/positive", func(t *testing.T) {
			brief := loadBriefByName(t, tc.brief)
			finding := lintMap(t, brief, tc.mutate(cleanLintHTML))[tc.rule]
			if finding.Status != lintFail {
				t.Fatalf("%s status = %s (%s), want FAIL", tc.rule, finding.Status, finding.Evidence)
			}
			if finding.Evidence == "" || len([]rune(finding.Evidence)) > 80 {
				t.Errorf("evidence %q must be non-empty and <= 80 chars", finding.Evidence)
			}
		})
		t.Run(tc.name+"/negative", func(t *testing.T) {
			brief := loadBriefByName(t, tc.brief)
			finding := lintMap(t, brief, cleanLintHTML)[tc.rule]
			if finding.Status != lintPass {
				t.Fatalf("%s status = %s (%s), want PASS", tc.rule, finding.Status, finding.Evidence)
			}
		})
	}
}

func TestLintHoursAcceptsValuesFromBrief(t *testing.T) {
	full := loadBriefByName(t, "full")
	for _, snippet := range []string{
		`<p>Senin 09.00-21.00</p>`,
		`<p>Rabu 09.00-21.00</p>`,
	} {
		doc := strings.Replace(cleanLintHTML, "</body>", snippet+"</body>", 1)
		if finding := lintMap(t, full, doc)[ruleHours]; finding.Status != lintPass {
			t.Fatalf("allowed brief hours %q = %s (%s), want PASS", snippet, finding.Status, finding.Evidence)
		}
	}
}

func TestLintIgnoresFontURLWeightSyntax(t *testing.T) {
	full := loadBriefByName(t, "full")
	doc := strings.Replace(cleanLintHTML,
		`family=Fraunces&display=swap`,
		`family=Fraunces:wght@400;700&display=swap`, 1)
	if finding := lintMap(t, full, doc)[ruleEmail]; finding.Status != lintPass {
		t.Fatalf("wght@ syntax = %s (%s), want PASS", finding.Status, finding.Evidence)
	}
}

func TestLintDesignPlanOver100WordsFails(t *testing.T) {
	full := loadBriefByName(t, "full")
	comment := "<!--\nDESIGN PLAN: " + strings.Repeat("kata ", 110) + "\nFACTS NAME: Fiktif EMAIL: none\n-->"
	doc := commentRE.ReplaceAllString(cleanLintHTML, "")
	doc = strings.Replace(doc, "</head>", comment+"</head>", 1)
	finding := lintMap(t, full, doc)[ruleDesignPlan]
	if finding.Status != lintFail {
		t.Fatalf("long DESIGN PLAN = %s (%s), want FAIL", finding.Status, finding.Evidence)
	}
}

func TestLintIgnoresCommentsScriptsAndStyles(t *testing.T) {
	full := loadBriefByName(t, "full")
	doc := strings.Replace(cleanLintHTML, "</head>",
		`<script>const x = "terbaik 09.00 bad@example.test";</script>`+
			`<style>.x { font-style: italic; }</style></head>`, 1)
	doc = strings.Replace(doc, "<body", `<!-- terbaik 09.00 bad@example.test --><body`, 1)
	findings := lintMap(t, full, doc)
	for _, rule := range []string{ruleEmail, ruleHours, ruleForbidden, ruleItalic} {
		if findings[rule].Status != lintPass {
			t.Errorf("%s status = %s (%s), want PASS inside ignored regions",
				rule, findings[rule].Status, findings[rule].Evidence)
		}
	}
}

func TestLintWarnings(t *testing.T) {
	full := loadBriefByName(t, "full")

	accent := strings.Replace(cleanLintHTML, `<section class="bg-ink"><p class="text-accent">Fiktif</p></section>`,
		`<section class="bg-paper"><p class="text-accent">Fiktif</p></section>`, 1)
	if finding := lintMap(t, full, accent)[ruleAccentText]; finding.Status != lintWarn {
		t.Errorf("accent outside ink = %s, want WARN", finding.Status)
	}

	paper := strings.Replace(cleanLintHTML, `<section class="bg-tint">`, `<section class="bg-paper">`, 1)
	if finding := lintMap(t, full, paper)[rulePaperRepeat]; finding.Status != lintWarn {
		t.Errorf("consecutive paper = %s, want WARN", finding.Status)
	}

	fewer := cleanLintHTML
	for i := 0; i < 2; i++ {
		start := strings.Index(fewer, "<section")
		end := strings.Index(fewer[start:], "</section>")
		if start < 0 || end < 0 {
			t.Fatal("cannot remove section for test")
		}
		fewer = fewer[:start] + fewer[start+end+len("</section>"):]
	}
	if finding := lintMap(t, full, fewer)[ruleSectionCnt]; finding.Status != lintWarn {
		t.Errorf("section count = %s, want WARN", finding.Status)
	}
}

func TestLintDirectoryWritesReportAndFails(t *testing.T) {
	briefs, err := loadBriefs("")
	if err != nil {
		t.Fatalf("loadBriefs: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "full_model_default_run1.html"), []byte(cleanLintHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(cleanLintHTML, "</body>", `<a href="#kontak">Hubungi Kami</a></body>`, 1)
	if err := os.WriteFile(filepath.Join(dir, "minimal_model_default_run1.html"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	results, hasFailure, err := lintDirectory(dir, briefs)
	if err != nil {
		t.Fatalf("lintDirectory: %v", err)
	}
	if !hasFailure {
		t.Fatal("hasFailure = false, want true")
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if _, err := writeLintReport(dir, results); err != nil {
		t.Fatalf("writeLintReport: %v", err)
	}
	report, err := os.ReadFile(filepath.Join(dir, "lint_report.md"))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(string(report), "| File | Brief | Rule | Status | Evidence |") {
		t.Error("report missing markdown table header")
	}
}
