package main

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type lintStatus string

const (
	lintPass lintStatus = "PASS"
	lintFail lintStatus = "FAIL"
	lintWarn lintStatus = "WARN"
)

type lintFinding struct {
	Rule     string
	Status   lintStatus
	Evidence string
}

type lintFileResult struct {
	File     string
	Brief    string
	Findings []lintFinding
}

func (r lintFileResult) hasFailure() bool {
	for _, f := range r.Findings {
		if f.Status == lintFail {
			return true
		}
	}
	return false
}

const (
	ruleEmail       = "email"
	rulePhone       = "phone/whatsapp"
	ruleHours       = "hours/days"
	ruleForbidden   = "forbidden-claims"
	ruleOpenStatus  = "permanent-open-status"
	ruleContactCTA  = "contact-cta"
	ruleDesignPlan  = "design-plan/FACTS"
	ruleItalic      = "italic"
	ruleExternal    = "external-assets"
	rulePalette     = "palette-hex"
	ruleAccentText  = "accent-outside-ink"
	rulePaperRepeat = "consecutive-paper"
	ruleSectionCnt  = "section-count"
)

var (
	commentRE   = regexp.MustCompile(`(?s)<!--(.*?)-->`)
	scriptRE    = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	styleRE     = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	tagRE       = regexp.MustCompile(`(?s)<[^>]+>`)
	scriptSrcRE = regexp.MustCompile(`(?is)<script\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)
	linkTagRE   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRE      = regexp.MustCompile(`(?is)\b([a-z0-9:_-]+)\s*=\s*["']([^"']*)["']`)
	anchorRE    = regexp.MustCompile(`(?is)<(?:a|button)\b[^>]*>(.*?)</(?:a|button)>`)
	ariaLabelRE = regexp.MustCompile(`(?is)\baria-label\s*=\s*["']([^"']+)["']`)
	sectionRE   = regexp.MustCompile(`(?is)<section\b[^>]*>.*?</section>`)
	sectionTag  = regexp.MustCompile(`(?is)<section\b[^>]*>`)
	waLinkRE    = regexp.MustCompile(`(?i)(?:wa\.me/|api\.whatsapp\.com/send\?[^"'\s]*phone=)([0-9]+)`)
	telLinkRE   = regexp.MustCompile(`(?i)tel:([+0-9()\-.\s]{7,})`)
	cssURLRE    = regexp.MustCompile(`(?is)@import\s+(?:url\()?\s*["']?([^"')\s]+)`)
	openNowRE   = regexp.MustCompile(`(?is)\b(buka|open)\b[^.!?\n]{0,20}\b(sekarang|hari ini|now)\b`)
)

var forbiddenClaims = []string{
	"terbaik", "berkualitas tinggi", "premium", "otentik", "istimewa",
	"sempurna", "sepenuh hati", "dengan cinta", "presisi", "artisan",
	"artisanal", "single origin", "petani lokal", "per batch",
	"sesuai permintaan", "ramah", "profesional", "terpercaya",
}

var permanentOpenPhrases = []string{
	"buka sekarang", "kedai buka", "buka hari ini", "sedang buka",
	"open now", "we are open", "we're open",
}

var contactCTAPhrases = []string{
	"hubungi kami", "kontak kami", "contact us", "hubungi", "kontak",
	"chat kami", "whatsapp", "telepon", "call us", "email kami",
}

func lintHTML(doc string, brief Brief) []lintFinding {
	ignored := stripIgnored(doc)
	visible := visibleText(ignored)
	return []lintFinding{
		checkEmail(ignored, brief.Facts),
		checkPhone(ignored, visible, brief.Facts),
		checkHours(visible, brief.Facts),
		checkForbidden(visible),
		checkPermanentOpen(visible, brief.Facts),
		checkContactCTA(ignored, brief.Facts),
		checkDesignPlan(doc),
		checkItalic(ignored),
		checkExternalAssets(doc),
		checkPalette(doc, brief.Facts),
		warnAccentText(ignored),
		warnConsecutivePaper(ignored),
		warnSectionCount(ignored),
	}
}

func stripIgnored(doc string) string {
	doc = commentRE.ReplaceAllString(doc, " ")
	doc = scriptRE.ReplaceAllString(doc, " ")
	doc = styleRE.ReplaceAllString(doc, " ")
	return doc
}

func stripComments(doc string) string {
	return commentRE.ReplaceAllString(doc, " ")
}

func visibleText(doc string) string {
	text := tagRE.ReplaceAllString(doc, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

func finding(rule string, status lintStatus, evidence string) lintFinding {
	if evidence == "" {
		switch status {
		case lintPass:
			evidence = "ok"
		case lintWarn:
			evidence = "heuristic warning"
		default:
			evidence = "violation"
		}
	}
	return lintFinding{Rule: rule, Status: status, Evidence: quoteEvidence(evidence)}
}

func quoteEvidence(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 80 {
		return string(runes[:77]) + "..."
	}
	return value
}

func firstUnknown(value string, allowed map[string]bool) (string, bool) {
	if value == "" || allowed[strings.ToLower(value)] {
		return "", false
	}
	return value, true
}

func checkEmail(doc string, facts BriefFacts) lintFinding {
	allowed := make(map[string]bool, len(facts.Emails))
	for _, email := range facts.Emails {
		allowed[email] = true
	}
	for _, email := range emailPattern.FindAllString(doc, -1) {
		if bad, ok := firstUnknown(email, allowed); ok {
			return finding(ruleEmail, lintFail, bad)
		}
	}
	return finding(ruleEmail, lintPass, "semua email berasal dari brief")
}

func checkPhone(doc, visible string, facts BriefFacts) lintFinding {
	allowed := make(map[string]bool)
	if facts.WhatsApp != "" {
		allowed[facts.WhatsApp] = true
	}
	var candidates []string
	for _, m := range waLinkRE.FindAllStringSubmatch(doc, -1) {
		candidates = append(candidates, "wa.me/"+m[1])
	}
	for _, m := range telLinkRE.FindAllStringSubmatch(doc, -1) {
		candidates = append(candidates, "tel:"+strings.TrimSpace(m[1]))
	}
	for _, phone := range phonePattern.FindAllString(visible, -1) {
		candidates = append(candidates, phone)
	}
	for _, candidate := range candidates {
		raw := candidate
		if idx := strings.LastIndexAny(candidate, "/:"); idx >= 0 && idx < len(candidate)-1 {
			raw = candidate[idx+1:]
		}
		phone := normalizePhone(raw)
		if phrase, ok := firstUnknown(phone, allowed); ok {
			return finding(rulePhone, lintFail, candidate+" => "+phrase)
		}
	}
	if facts.WhatsApp == "" {
		return finding(rulePhone, lintPass, "brief tanpa WhatsApp dan HTML tanpa nomor")
	}
	return finding(rulePhone, lintPass, "nomor cocok dengan WhatsApp brief")
}

func checkHours(visible string, facts BriefFacts) lintFinding {
	var bad []string
	if !facts.HasHours {
		for _, m := range timePattern.FindAllString(visible, -1) {
			bad = append(bad, "jam "+m)
		}
		for _, day := range dayPattern.FindAllString(visible, -1) {
			bad = append(bad, "hari "+day)
		}
		if len(bad) > 0 {
			return finding(ruleHours, lintFail, strings.Join(bad, ", "))
		}
		return finding(ruleHours, lintPass, "brief tanpa jam dan HTML tanpa jam/hari")
	}

	allowedTimes := facts.Times
	for _, m := range timePattern.FindAllStringSubmatch(visible, -1) {
		timeValue := canonicalTime(m[1], m[2])
		if !allowedTimes[timeValue] {
			bad = append(bad, "jam "+m[0])
		}
	}
	allowedDays := facts.Days
	for _, day := range dayPattern.FindAllString(visible, -1) {
		key := strings.ReplaceAll(strings.ToLower(day), "'", "")
		if !facts.AllDays && !allowedDays[key] {
			bad = append(bad, "hari "+day)
		}
	}
	if len(bad) > 0 {
		return finding(ruleHours, lintFail, strings.Join(bad, ", "))
	}
	return finding(ruleHours, lintPass, "jam/hari berasal dari brief")
}

func checkForbidden(visible string) lintFinding {
	lower := strings.ToLower(visible)
	for _, phrase := range forbiddenClaims {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			start := idx - 30
			if start < 0 {
				start = 0
			}
			end := idx + len(phrase) + 30
			if end > len(visible) {
				end = len(visible)
			}
			return finding(ruleForbidden, lintFail, visible[start:end])
		}
	}
	return finding(ruleForbidden, lintPass, "tidak ada kata klaim terlarang")
}

func checkPermanentOpen(visible string, facts BriefFacts) lintFinding {
	if facts.HasHours {
		return finding(ruleOpenStatus, lintPass, "brief memiliki jam buka")
	}
	lower := strings.ToLower(visible)
	for _, phrase := range permanentOpenPhrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return finding(ruleOpenStatus, lintFail, excerptAt(visible, idx, len(phrase)))
		}
	}
	if loc := openNowRE.FindStringIndex(visible); loc != nil {
		return finding(ruleOpenStatus, lintFail, visible[loc[0]:loc[1]])
	}
	return finding(ruleOpenStatus, lintPass, "tidak ada pill buka permanen")
}

func checkContactCTA(doc string, facts BriefFacts) lintFinding {
	if facts.HasContact {
		return finding(ruleContactCTA, lintPass, "brief memiliki saluran kontak")
	}
	for _, m := range anchorRE.FindAllStringSubmatch(doc, -1) {
		label := strings.ToLower(strings.Join(strings.Fields(visibleText(m[1])), " "))
		if label == "" {
			continue
		}
		for _, phrase := range contactCTAPhrases {
			if strings.Contains(label, phrase) {
				return finding(ruleContactCTA, lintFail, label)
			}
		}
	}
	for _, m := range ariaLabelRE.FindAllStringSubmatch(doc, -1) {
		label := strings.ToLower(m[1])
		for _, phrase := range contactCTAPhrases {
			if strings.Contains(label, phrase) {
				return finding(ruleContactCTA, lintFail, "aria-label: "+label)
			}
		}
	}
	return finding(ruleContactCTA, lintPass, "tidak ada CTA kontak")
}

func checkDesignPlan(doc string) lintFinding {
	for _, m := range commentRE.FindAllStringSubmatch(doc, -1) {
		body := m[1]
		if !strings.Contains(strings.ToLower(body), "design plan") {
			continue
		}
		lines := strings.Split(body, "\n")
		hasFacts := false
		words := 0
		for _, line := range lines {
			upper := strings.ToUpper(line)
			if strings.Contains(upper, "FACTS") {
				if strings.Contains(upper, "EMAIL:") {
					hasFacts = true
				}
				continue
			}
			words += len(strings.Fields(line))
		}
		if !hasFacts {
			return finding(ruleDesignPlan, lintFail, "DESIGN PLAN tanpa FACTS EMAIL:")
		}
		if words > 100 {
			return finding(ruleDesignPlan, lintFail, fmt.Sprintf("DESIGN PLAN %d kata (maks 100)", words))
		}
		return finding(ruleDesignPlan, lintPass, fmt.Sprintf("DESIGN PLAN %d kata + FACTS EMAIL:", words))
	}
	return finding(ruleDesignPlan, lintFail, "komentar DESIGN PLAN tidak ditemukan")
}

func checkItalic(doc string) lintFinding {
	if loc := regexp.MustCompile(`(?i)<(?:em|i)\b`).FindStringIndex(doc); loc != nil {
		return finding(ruleItalic, lintFail, doc[loc[0]:loc[1]])
	}
	if loc := regexp.MustCompile(`(?i)font-style\s*:\s*italic`).FindStringIndex(doc); loc != nil {
		return finding(ruleItalic, lintFail, doc[loc[0]:loc[1]])
	}
	classRE := regexp.MustCompile(`(?is)\bclass\s*=\s*["']([^"']*)["']`)
	for _, m := range classRE.FindAllStringSubmatch(doc, -1) {
		for _, class := range strings.Fields(m[1]) {
			value := strings.ToLower(class)
			if value == "not-italic" {
				continue
			}
			if value == "italic" || strings.HasSuffix(value, ":italic") {
				return finding(ruleItalic, lintFail, "class="+class)
			}
		}
	}
	return finding(ruleItalic, lintPass, "tidak ada italic")
}

func checkExternalAssets(doc string) lintFinding {
	doc = stripComments(doc)
	for _, m := range scriptSrcRE.FindAllStringSubmatch(doc, -1) {
		if !allowedHost(m[1], "cdn.tailwindcss.com") {
			return finding(ruleExternal, lintFail, "script "+m[1])
		}
	}
	for _, tag := range linkTagRE.FindAllString(doc, -1) {
		attrs := parseAttrs(tag)
		if !strings.Contains(strings.ToLower(attrs["rel"]), "stylesheet") {
			continue
		}
		href := attrs["href"]
		if !allowedHost(href, "fonts.googleapis.com", "fonts.gstatic.com") {
			return finding(ruleExternal, lintFail, "stylesheet "+href)
		}
	}
	for _, m := range cssURLRE.FindAllStringSubmatch(doc, -1) {
		if strings.HasPrefix(strings.ToLower(m[1]), "http") && !allowedHost(m[1], "fonts.googleapis.com", "fonts.gstatic.com") {
			return finding(ruleExternal, lintFail, "@import "+m[1])
		}
	}
	return finding(ruleExternal, lintPass, "hanya Tailwind CDN dan Google Fonts")
}

func parseAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, m := range attrRE.FindAllStringSubmatch(tag, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	return attrs
}

func allowedHost(raw string, hosts ...string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range hosts {
		if host == allowed {
			return true
		}
	}
	return false
}

func checkPalette(doc string, facts BriefFacts) lintFinding {
	allowed := allowedPalette(facts)
	for _, color := range hexPattern.FindAllString(stripComments(doc), -1) {
		if !allowed[strings.ToLower(color)] {
			return finding(rulePalette, lintFail, color)
		}
	}
	return finding(rulePalette, lintPass, "semua hex adalah token palet")
}

func warnAccentText(doc string) lintFinding {
	type sectionSpan struct {
		start, end int
		ink        bool
	}
	var spans []sectionSpan
	for _, loc := range sectionRE.FindAllStringIndex(doc, -1) {
		block := doc[loc[0]:loc[1]]
		open := sectionTag.FindString(block)
		attrs := parseAttrs(open)
		spans = append(spans, sectionSpan{start: loc[0], end: loc[1], ink: hasClass(attrs["class"], "bg-ink")})
	}
	for _, loc := range regexp.MustCompile(`(?i)text-accent`).FindAllStringIndex(doc, -1) {
		insideInk := false
		for _, span := range spans {
			if loc[0] >= span.start && loc[0] < span.end {
				if span.ink {
					insideInk = true
				}
				break
			}
		}
		if !insideInk {
			return finding(ruleAccentText, lintWarn, excerptAt(doc, loc[0], loc[1]-loc[0]))
		}
	}
	return finding(ruleAccentText, lintPass, "text-accent hanya di section bg-ink")
}

func warnConsecutivePaper(doc string) lintFinding {
	var previousPaper bool
	for _, open := range sectionTag.FindAllString(doc, -1) {
		attrs := parseAttrs(open)
		paper := hasClass(attrs["class"], "bg-paper")
		if paper && previousPaper {
			return finding(rulePaperRepeat, lintWarn, open)
		}
		previousPaper = paper
	}
	return finding(rulePaperRepeat, lintPass, "tidak ada section paper berurutan")
}

func warnSectionCount(doc string) lintFinding {
	count := len(sectionTag.FindAllString(doc, -1))
	if count < 5 || count > 7 {
		return finding(ruleSectionCnt, lintWarn, fmt.Sprintf("%d section (disarankan 5-7)", count))
	}
	return finding(ruleSectionCnt, lintPass, fmt.Sprintf("%d section", count))
}

func hasClass(classes, wanted string) bool {
	for _, class := range strings.Fields(classes) {
		if strings.EqualFold(class, wanted) {
			return true
		}
	}
	return false
}

func excerptAt(value string, start, length int) string {
	if start < 0 {
		start = 0
	}
	end := start + length + 40
	if end > len(value) {
		end = len(value)
	}
	begin := start - 20
	if begin < 0 {
		begin = 0
	}
	return value[begin:end]
}

func printLintResult(w io.Writer, result lintFileResult) {
	fmt.Fprintf(w, "\nLint %s (brief: %s)\n", result.File, result.Brief)
	for _, f := range result.Findings {
		fmt.Fprintf(w, "  %-4s %-24s %s\n", f.Status, f.Rule, f.Evidence)
	}
}

func writeLintReport(dir string, results []lintFileResult) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "lint_report.md")
	var out strings.Builder
	out.WriteString("# AIBench Lint Report\n\n")
	out.WriteString("Generated: " + time.Now().Format(time.RFC3339) + "\n\n")
	out.WriteString("| File | Brief | Rule | Status | Evidence |\n")
	out.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, result := range results {
		for _, f := range result.Findings {
			fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n",
				escapeMarkdown(result.File), escapeMarkdown(result.Brief),
				escapeMarkdown(f.Rule), f.Status, escapeMarkdown(f.Evidence))
		}
	}
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func lintDirectory(dir string, briefs []Brief) ([]lintFileResult, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var results []lintFileResult
	hasFailure := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".html") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return results, hasFailure, err
		}
		brief, ok := briefForFilename(entry.Name(), briefs)
		if !ok {
			result := lintFileResult{
				File:  entry.Name(),
				Brief: "unknown",
				Findings: []lintFinding{{
					Rule:     "brief-fixture",
					Status:   lintFail,
					Evidence: "nama file tidak cocok dengan brief yang dikenal",
				}},
			}
			results = append(results, result)
			hasFailure = true
			continue
		}
		result := lintFileResult{File: entry.Name(), Brief: brief.Name, Findings: lintHTML(string(data), brief)}
		if result.hasFailure() {
			hasFailure = true
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, false, fmt.Errorf("tidak ada file .html di %s", dir)
	}
	return results, hasFailure, nil
}

func briefForFilename(filename string, briefs []Brief) (Brief, bool) {
	for _, brief := range briefs {
		if strings.HasPrefix(filename, brief.Name+"_") {
			return brief, true
		}
	}
	return Brief{}, false
}
