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
	ruleNoChannel   = "no-channel-text"
	ruleInventedDay = "invented-days"
	ruleRepetition  = "repetition"
	ruleEnglishUI   = "english-ui"
	rulePlanPage    = "plan-vs-page"
	ruleDeadLink    = "dead-link-target"
)

var (
	commentRE     = regexp.MustCompile(`(?s)<!--(.*?)-->`)
	scriptRE      = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	styleRE       = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	tagRE         = regexp.MustCompile(`(?s)<[^>]+>`)
	scriptSrcRE   = regexp.MustCompile(`(?is)<script\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)
	linkTagRE     = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRE        = regexp.MustCompile(`(?is)\b([a-z0-9:_-]+)\s*=\s*["']([^"']*)["']`)
	anchorRE      = regexp.MustCompile(`(?is)<(?:a|button)\b[^>]*>(.*?)</(?:a|button)>`)
	ariaLabelRE   = regexp.MustCompile(`(?is)\baria-label\s*=\s*["']([^"']+)["']`)
	sectionRE     = regexp.MustCompile(`(?is)<section\b[^>]*>.*?</section>`)
	sectionTag    = regexp.MustCompile(`(?is)<section\b[^>]*>`)
	classAttrRE   = regexp.MustCompile(`(?is)\bclass\s*=\s*["']([^"']*)["']`)
	cssPropertyRE = regexp.MustCompile(`(?is)([a-z-]+)\s*:\s*[^;{}<>]*$`)
	tagScanRE     = regexp.MustCompile(`(?is)<(/?)\s*([a-zA-Z][a-zA-Z0-9:-]*)([^>]*)>`)
	wordRE        = regexp.MustCompile(`(?i)[\p{L}\p{N}']+`)
	planPoint2RE  = regexp.MustCompile(`(?i)^\s*(?:[-*]\s*)?\(?2[.)]\s*(.*)$`)
	planWidgetRE  = regexp.MustCompile(`(?i)\b(widget|tab|tabs|kalkulator|calculator)\b`)
	buttonRE      = regexp.MustCompile(`(?is)<button\b([^>]*)>(.*?)</button>`)
	anchorAttrsRE = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	sectionAttrRE = regexp.MustCompile(`(?is)<section\b([^>]*)>(.*?)</section>`)
	brandNameRE   = regexp.MustCompile(`(?im)^\s*Business/brand name:\s*(.+)$`)
	moneyRE       = regexp.MustCompile(`(?i)\brp\s*[0-9][0-9.]*`)
	addressRE     = regexp.MustCompile(`(?i)\b(?:jalan|jl\.)\s+[^,.\n]+`)
	waLinkRE      = regexp.MustCompile(`(?i)(?:wa\.me/|api\.whatsapp\.com/send\?[^"'\s]*phone=)([0-9]+)`)
	telLinkRE     = regexp.MustCompile(`(?i)tel:([+0-9()\-.\s]{7,})`)
	cssURLRE      = regexp.MustCompile(`(?is)@import\s+(?:url\()?\s*["']?([^"')\s]+)`)
	openNowRE     = regexp.MustCompile(`(?is)\b(buka|open)\b[^.!?\n]{0,20}\b(sekarang|hari ini|now)\b`)
)

type htmlTag struct {
	start       int
	end         int
	name        string
	closing     bool
	selfClosing bool
	attrs       map[string]string
}

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

func scanTags(doc string) []htmlTag {
	var tags []htmlTag
	for _, m := range tagScanRE.FindAllStringSubmatchIndex(doc, -1) {
		raw := doc[m[0]:m[1]]
		tag := htmlTag{
			start:   m[0],
			end:     m[1],
			name:    doc[m[4]:m[5]],
			closing: doc[m[2]:m[3]] == "/",
			attrs:   parseAttrs(raw),
		}
		tag.selfClosing = strings.HasSuffix(strings.TrimSpace(doc[m[6]:m[7]]), "/")
		tags = append(tags, tag)
	}
	return tags
}

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

var noChannelPhrases = []string{"hubungi", "kontak kami", "tanya", "pesan via", "chat"}

var inventedDayPhrases = []string{"setiap hari", "tiap hari", "libur", "tutup"}

var englishUIPhrases = []string{
	"all rights reserved", "freelance", "graphic designer", "visual identity",
	"contact us", "about us", "learn more",
}

func lintHTML(doc string, brief Brief) []lintFinding {
	ignored := stripIgnored(doc)
	visible := visibleText(ignored)
	findings := []lintFinding{
		checkEmail(ignored, brief.Facts),
		checkPhone(ignored, visible, brief.Facts),
		checkHours(visible, brief.Facts),
		checkForbidden(visible),
		checkPermanentOpen(visible, brief.Facts),
		checkContactCTA(ignored, brief.Facts),
		checkNoChannelText(visible, brief.Facts),
		checkInventedDays(visible, brief.Facts),
		checkDesignPlan(doc),
		checkItalic(ignored),
		checkExternalAssets(doc),
		warnAccentText(ignored),
		warnConsecutivePaper(ignored),
		warnSectionCount(ignored),
		checkRepetition(visible),
		checkEnglishUI(visible, brief),
		checkPlanVsPage(doc),
		checkDeadLinkTarget(ignored, brief),
	}
	findings = append(findings, checkPalette(doc, brief.Facts)...)
	return findings
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

func checkNoChannelText(visible string, facts BriefFacts) lintFinding {
	if facts.HasAnyChannel() {
		return finding(ruleNoChannel, lintPass, "brief memiliki WhatsApp, email, alamat, atau telepon")
	}
	lower := strings.ToLower(visible)
	for _, phrase := range noChannelPhrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return finding(ruleNoChannel, lintFail, excerptAt(visible, idx, len(phrase)))
		}
	}
	return finding(ruleNoChannel, lintPass, "brief tanpa saluran kontak dan teks tanpa ajakan kontak")
}

func checkInventedDays(visible string, facts BriefFacts) lintFinding {
	if len(facts.Days) > 0 || facts.AllDays {
		return finding(ruleInventedDay, lintPass, "brief menyebut nama hari atau semua hari")
	}
	lower := strings.ToLower(visible)
	if loc := dayPattern.FindStringIndex(lower); loc != nil {
		return finding(ruleInventedDay, lintFail, "hari "+excerptAt(visible, loc[0], loc[1]-loc[0]))
	}
	for _, phrase := range inventedDayPhrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return finding(ruleInventedDay, lintFail, excerptAt(visible, idx, len(phrase)))
		}
	}
	return finding(ruleInventedDay, lintPass, "brief tanpa nama hari dan teks tanpa hari/libur/tutup")
}

func checkRepetition(visible string) lintFinding {
	words := wordRE.FindAllString(strings.ToLower(visible), -1)
	counts := make(map[string]int)
	for i := 0; i+4 <= len(words); i++ {
		counts[strings.Join(words[i:i+4], " ")]++
	}
	bestPhrase := ""
	bestCount := 0
	for i := 0; i+4 <= len(words); i++ {
		phrase := words[i : i+4]
		if counts[strings.Join(phrase, " ")] <= 2 {
			continue
		}
		for i+len(phrase) < len(words) && len(phrase) < 12 {
			candidate := append(append([]string{}, phrase...), words[i+len(phrase)])
			if countWordSequence(words, candidate) <= 2 {
				break
			}
			phrase = candidate
		}
		count := countWordSequence(words, phrase)
		if count > bestCount || (count == bestCount && len(phrase) > len(strings.Fields(bestPhrase))) {
			bestPhrase = strings.Join(phrase, " ")
			bestCount = count
		}
	}
	if bestCount > 2 {
		return finding(ruleRepetition, lintWarn, fmt.Sprintf("%dx frasa %q", bestCount, bestPhrase))
	}
	return finding(ruleRepetition, lintPass, "tidak ada frasa 4+ kata yang muncul lebih dari 2 kali")
}

func countWordSequence(words, phrase []string) int {
	if len(phrase) == 0 || len(phrase) > len(words) {
		return 0
	}
	count := 0
	for i := 0; i+len(phrase) <= len(words); i++ {
		matched := true
		for j := range phrase {
			if words[i+j] != phrase[j] {
				matched = false
				break
			}
		}
		if matched {
			count++
		}
	}
	return count
}

func checkEnglishUI(visible string, brief Brief) lintFinding {
	if !strings.Contains(strings.ToLower(brief.Text), "content language: id") {
		return finding(ruleEnglishUI, lintPass, "brief bukan berbahasa Indonesia")
	}
	lower := strings.ToLower(visible)
	for _, phrase := range englishUIPhrases {
		if idx := strings.Index(lower, phrase); idx >= 0 {
			return finding(ruleEnglishUI, lintWarn, excerptAt(visible, idx, len(phrase)))
		}
	}
	return finding(ruleEnglishUI, lintPass, "tidak ada frasa UI Inggris")
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

func checkPlanVsPage(doc string) lintFinding {
	plan, ok := designPlanBody(doc)
	if !ok {
		return finding(rulePlanPage, lintPass, "DESIGN PLAN tidak ditemukan")
	}
	pointTwo := designPlanPointTwo(plan)
	if !planWidgetRE.MatchString(pointTwo) {
		return finding(rulePlanPage, lintPass, "butir (2) tidak menyebut widget/tab/kalkulator")
	}
	if hasNonMenuInteractive(stripIgnored(doc)) {
		return finding(rulePlanPage, lintPass, "rencana interaktif didukung elemen interaktif")
	}
	return finding(rulePlanPage, lintWarn, "butir (2) menyebut widget/tab/kalkulator tanpa elemen interaktif selain tombol menu: "+pointTwo)
}

func designPlanBody(doc string) (string, bool) {
	for _, m := range commentRE.FindAllStringSubmatch(doc, -1) {
		if strings.Contains(strings.ToLower(m[1]), "design plan") {
			return m[1], true
		}
	}
	return "", false
}

func designPlanPointTwo(plan string) string {
	lines := strings.Split(plan, "\n")
	for _, line := range lines {
		if m := planPoint2RE.FindStringSubmatch(line); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	started := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !started {
			if strings.Contains(strings.ToLower(trimmed), "design plan") {
				started = true
			}
			continue
		}
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hasNonMenuInteractive(doc string) bool {
	for _, tag := range scanTags(doc) {
		if tag.closing {
			continue
		}
		name := strings.ToLower(tag.name)
		if name == "input" || name == "select" {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(tag.attrs["role"]), "tab") {
			return true
		}
	}
	for _, m := range buttonRE.FindAllStringSubmatch(doc, -1) {
		attrs := parseAttrs(m[1])
		text := visibleText(m[2])
		if isMenuButton(attrs, text) {
			continue
		}
		return true
	}
	return false
}

func isMenuButton(attrs map[string]string, text string) bool {
	combined := strings.ToLower(strings.Join([]string{
		attrs["class"], attrs["id"], attrs["aria-label"], attrs["aria-controls"],
		attrs["data-target"], attrs["data-menu"], attrs["onclick"], text,
	}, " "))
	for _, marker := range []string{"menu", "hamburger", "nav-toggle", "navigation"} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

func checkDeadLinkTarget(doc string, brief Brief) lintFinding {
	targets := map[string]bool{}
	for _, m := range anchorAttrsRE.FindAllStringSubmatch(doc, -1) {
		addAnchorTarget(targets, parseAttrs(m[1])["href"])
	}
	for _, m := range buttonRE.FindAllStringSubmatch(doc, -1) {
		attrs := parseAttrs(m[1])
		addAnchorTarget(targets, attrs["data-target"])
		addAnchorTarget(targets, attrs["data-href"])
	}
	if len(targets) == 0 {
		return finding(ruleDeadLink, lintPass, "tidak ada tautan anchor ke section")
	}
	sections := make(map[string]string)
	for _, m := range sectionAttrRE.FindAllStringSubmatch(doc, -1) {
		id := strings.TrimSpace(parseAttrs(m[1])["id"])
		if id != "" {
			sections[id] = visibleText(m[2])
		}
	}
	for id := range targets {
		text, ok := sections[id]
		if !ok {
			continue
		}
		if !sectionHasBriefFacts(text, brief) {
			return finding(ruleDeadLink, lintWarn, fmt.Sprintf("#%s: %s", id, excerptAt(text, 0, len(text))))
		}
	}
	return finding(ruleDeadLink, lintPass, "semua tautan anchor menuju section berisi fakta brief")
}

func addAnchorTarget(targets map[string]bool, href string) {
	value := strings.TrimSpace(href)
	if !strings.HasPrefix(value, "#") || len(value) < 2 {
		return
	}
	targets[strings.TrimPrefix(value, "#")] = true
}

func sectionHasBriefFacts(section string, brief Brief) bool {
	lower := strings.ToLower(section)
	facts := brief.Facts
	for _, email := range facts.Emails {
		if strings.Contains(lower, strings.ToLower(email)) {
			return true
		}
	}
	if facts.WhatsApp != "" && strings.Contains(normalizePhone(section), facts.WhatsApp) {
		return true
	}
	for _, phone := range facts.Phones {
		if phone != "" && strings.Contains(normalizePhone(section), phone) {
			return true
		}
	}
	if len(facts.Days) > 0 || facts.AllDays {
		for _, day := range dayPattern.FindAllString(lower, -1) {
			key := strings.ReplaceAll(day, "'", "")
			if facts.AllDays || facts.Days[key] {
				return true
			}
		}
	}
	if len(facts.Times) > 0 {
		for _, m := range timePattern.FindAllStringSubmatch(section, -1) {
			if facts.Times[canonicalTime(m[1], m[2])] {
				return true
			}
		}
	}
	briefPrices := moneyRE.FindAllString(brief.Text, -1)
	for _, price := range briefPrices {
		if strings.Contains(normalizeDigits(lower), normalizeDigits(price)) {
			return true
		}
	}
	for _, address := range addressRE.FindAllString(brief.Text, -1) {
		if strings.Contains(lower, strings.ToLower(strings.TrimSpace(address))) {
			return true
		}
	}
	if m := brandNameRE.FindStringSubmatch(brief.Text); len(m) > 1 {
		if strings.Contains(lower, strings.ToLower(strings.TrimSpace(m[1]))) {
			return true
		}
	}
	return false
}

func normalizeDigits(value string) string {
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	return digits.String()
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

func checkPalette(doc string, facts BriefFacts) []lintFinding {
	allowed := allowedPalette(facts)
	partialPalette := facts.HasPalette && len(facts.Palette) < 5
	stripped := stripComments(doc)
	var findings []lintFinding
	for _, loc := range hexPattern.FindAllStringIndex(stripped, -1) {
		color := stripped[loc[0]:loc[1]]
		if allowed[strings.ToLower(color)] {
			continue
		}
		context := hexUsageContext(stripped, loc[0], loc[1])
		if partialPalette {
			if palette, ok := fallbackPaletteFor(color); ok {
				evidence := fmt.Sprintf("%s [%s] palet cadangan dicampur dengan warna brief (%s)",
					context, color, palette.Name)
				findings = append(findings, finding(rulePalette, lintWarn, evidence))
				continue
			}
		}
		findings = append(findings, finding(rulePalette, lintFail, fmt.Sprintf("%s [%s] di luar palet brief", context, color)))
	}
	if len(findings) == 0 {
		return []lintFinding{finding(rulePalette, lintPass, "semua hex adalah token palet")}
	}
	return findings
}

func hexUsageContext(doc string, start, end int) string {
	for _, m := range classAttrRE.FindAllStringSubmatchIndex(doc, -1) {
		valueStart, valueEnd := m[2], m[3]
		if start < valueStart || end > valueEnd {
			continue
		}
		value := doc[valueStart:valueEnd]
		relativeStart := start - valueStart
		relativeEnd := end - valueStart
		return `class="` + windowAround(value, relativeStart, relativeEnd) + `"`
	}
	prefix := doc[:start]
	if loc := cssPropertyRE.FindStringSubmatchIndex(prefix); loc != nil {
		property := strings.TrimSpace(prefix[loc[2]:loc[3]])
		valueStart := loc[1]
		value := strings.TrimSpace(doc[valueStart:end])
		return windowAround(property+": "+value, 0, len(property)+len(value))
	}
	return contextWindow(doc, start, end, 40)
}

func windowAround(value string, start, end int) string {
	const limit = 40
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	startRune := len([]rune(value[:start]))
	endRune := len([]rune(value[:end]))
	begin := startRune - (limit-(endRune-startRune))/2
	if begin < 0 {
		begin = 0
	}
	if begin+limit > len(runes) {
		begin = len(runes) - limit
	}
	if endRune-begin > limit {
		begin = endRune - limit
	}
	if begin < 0 {
		begin = 0
	}
	return string(runes[begin : begin+limit])
}

func contextWindow(value string, start, end, limit int) string {
	runes := []rune(value)
	startRune := len([]rune(value[:start]))
	endRune := len([]rune(value[:end]))
	begin := startRune - (limit-(endRune-startRune))/2
	if begin < 0 {
		begin = 0
	}
	if begin+limit > len(runes) {
		begin = len(runes) - limit
	}
	if begin < 0 {
		begin = 0
	}
	finish := begin + limit
	if finish > len(runes) {
		finish = len(runes)
	}
	return strings.Join(strings.Fields(string(runes[begin:finish])), " ")
}

func warnAccentText(doc string) lintFinding {
	var stack []htmlTag
	for _, tag := range scanTags(doc) {
		if tag.closing {
			popTag(&stack, tag.name)
			continue
		}
		if hasTextAccentClass(tag.attrs["class"]) {
			if backgroundClass(tag.attrs["class"], stack) != "bg-ink" {
				return finding(ruleAccentText, lintWarn, excerptAt(doc, tag.start, tag.end-tag.start))
			}
		}
		if !tag.selfClosing && !voidTags[strings.ToLower(tag.name)] {
			stack = append(stack, tag)
		}
	}
	return finding(ruleAccentText, lintPass, "text-accent hanya di dalam elemen bg-ink")
}

func popTag(stack *[]htmlTag, name string) {
	for len(*stack) > 0 {
		top := (*stack)[len(*stack)-1]
		*stack = (*stack)[:len(*stack)-1]
		if strings.EqualFold(top.name, name) {
			return
		}
	}
}

func hasTextAccentClass(classes string) bool {
	for _, class := range strings.Fields(classes) {
		if strings.Contains(strings.ToLower(class), "text-accent") {
			return true
		}
	}
	return false
}

func backgroundClass(classes string, stack []htmlTag) string {
	for _, class := range strings.Fields(classes) {
		lower := strings.ToLower(class)
		if lower == "bg-ink" || strings.HasPrefix(lower, "bg-ink/") {
			return "bg-ink"
		}
		if strings.HasPrefix(lower, "bg-") {
			return lower
		}
	}
	return nearestBackgroundClass(stack)
}

func nearestBackgroundClass(stack []htmlTag) string {
	for i := len(stack) - 1; i >= 0; i-- {
		for _, class := range strings.Fields(stack[i].attrs["class"]) {
			lower := strings.ToLower(class)
			if lower == "bg-ink" || strings.HasPrefix(lower, "bg-ink/") {
				return "bg-ink"
			}
			if strings.HasPrefix(lower, "bg-") {
				return lower
			}
		}
	}
	return ""
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
