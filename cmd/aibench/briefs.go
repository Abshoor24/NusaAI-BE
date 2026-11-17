package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const briefDir = "cmd/aibench/briefs"

var requiredBriefs = []string{"full", "service", "wa_only", "minimal", "nopalette"}

type Brief struct {
	Name  string
	Text  string
	Facts BriefFacts
}

type BriefFacts struct {
	Emails     []string
	WhatsApp   string
	Phones     []string
	Days       map[string]bool
	AllDays    bool
	Times      map[string]bool
	HasHours   bool
	HasContact bool
	HasAddress bool
	Palette    []string
	HasPalette bool
}

func (f BriefFacts) HasAnyChannel() bool {
	return f.HasContact || f.HasAddress || len(f.Emails) > 0 || len(f.Phones) > 0 || f.WhatsApp != ""
}

var (
	emailPattern    = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	whatsAppPattern = regexp.MustCompile(`(?i)whatsapp\s*(?:kami)?\s*[:=]?\s*(\+?[0-9(][0-9\s().\-]{7,}[0-9])`)
	phonePattern    = regexp.MustCompile(`(?:\+?62|0)8[0-9][0-9\s().\-]{6,}[0-9]`)
	timePattern     = regexp.MustCompile(`\b([01]?\d|2[0-3])[.:]([0-5]\d)\b`)
	dayPattern      = regexp.MustCompile(`(?i)\b(senin|selasa|rabu|kamis|jumat|jum'at|sabtu|minggu)\b`)
	palettePattern  = regexp.MustCompile(`(?im)^color palette:\s*(.+)$`)
	hexPattern      = regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)
)

func loadBriefs(dir string) ([]Brief, error) {
	if dir == "" {
		dir = defaultBriefDir()
	}
	briefs := make([]Brief, 0, len(requiredBriefs))
	for _, name := range requiredBriefs {
		path := filepath.Join(dir, "brief_"+name+".txt")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read brief fixture %s: %w", path, err)
		}
		text := string(data)
		briefs = append(briefs, Brief{Name: name, Text: text, Facts: parseBriefFacts(text)})
	}
	return briefs, nil
}

func defaultBriefDir() string {
	candidates := []string{briefDir}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "briefs"))
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "briefs"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return briefDir
}

func availableBriefNames(briefs []Brief) []string {
	names := make([]string, 0, len(briefs))
	for _, b := range briefs {
		names = append(names, b.Name)
	}
	return names
}

func selectBriefs(all []Brief, name string) ([]Brief, error) {
	if name == "" {
		return all, nil
	}
	for _, b := range all {
		if b.Name == name {
			return []Brief{b}, nil
		}
	}
	names := availableBriefNames(all)
	sort.Strings(names)
	return nil, fmt.Errorf("brief %q tidak dikenal; brief tersedia: %s", name, strings.Join(names, ", "))
}

func briefDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:8]
}

func parseBriefFacts(text string) BriefFacts {
	facts := BriefFacts{
		Days:    make(map[string]bool),
		Times:   make(map[string]bool),
		Palette: make([]string, 0, 4),
	}
	seenEmail := make(map[string]bool)
	for _, email := range emailPattern.FindAllString(text, -1) {
		email = strings.ToLower(email)
		if !seenEmail[email] {
			seenEmail[email] = true
			facts.Emails = append(facts.Emails, email)
		}
	}
	if m := whatsAppPattern.FindStringSubmatch(text); len(m) > 1 {
		facts.WhatsApp = normalizePhone(m[1])
	}
	seenPhone := make(map[string]bool)
	for _, match := range phonePattern.FindAllString(text, -1) {
		phone := normalizePhone(match)
		if phone != "" && !seenPhone[phone] {
			seenPhone[phone] = true
			facts.Phones = append(facts.Phones, phone)
		}
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "setiap hari") || strings.Contains(lower, "tiap hari") || strings.Contains(lower, "harian") ||
		strings.Contains(lower, "senin-minggu") || strings.Contains(lower, "senin sampai minggu") ||
		strings.Contains(lower, "senin s/d minggu") {
		facts.AllDays = true
	}
	for _, day := range dayPattern.FindAllString(lower, -1) {
		facts.Days[strings.ReplaceAll(day, "'", "")] = true
	}
	for _, m := range timePattern.FindAllStringSubmatch(text, -1) {
		facts.Times[canonicalTime(m[1], m[2])] = true
	}
	if strings.Contains(lower, "jam buka") || strings.Contains(lower, "jam operasional") {
		facts.HasHours = true
	}
	facts.HasHours = facts.HasHours || len(facts.Times) > 0

	if m := palettePattern.FindStringSubmatch(text); len(m) > 1 {
		for _, color := range hexPattern.FindAllString(m[1], -1) {
			facts.Palette = append(facts.Palette, strings.ToLower(color))
		}
		facts.HasPalette = len(facts.Palette) > 0
	}
	facts.HasContact = len(facts.Emails) > 0 || facts.WhatsApp != ""
	facts.HasAddress = strings.Contains(lower, "alamat:") || strings.Contains(lower, "alamat ") ||
		strings.Contains(lower, "jalan ") || strings.Contains(lower, "jl. ")
	return facts
}

func canonicalTime(hour, minute string) string {
	if len(hour) == 1 {
		hour = "0" + hour
	}
	return hour + minute
}

func normalizePhone(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	value := digits.String()
	if strings.HasPrefix(value, "0") {
		return "62" + value[1:]
	}
	return value
}

type paletteSet struct {
	Name   string
	Colors []string
}

var fallbackPalettes = []paletteSet{
	{Name: "Hangat", Colors: []string{"#2B1D14", "#F6EFE4", "#EADBC6", "#D9733A", "#9A4A1C"}},
	{Name: "Hijau", Colors: []string{"#12261C", "#F3F6EE", "#DCE8D2", "#8FCB7A", "#2F6B3A"}},
	{Name: "Malam", Colors: []string{"#0E1320", "#F4F5F7", "#E3E7EE", "#C6F24E", "#46630A"}},
	{Name: "Biru laut", Colors: []string{"#10223F", "#F7F3EC", "#E8E0D2", "#E0A93B", "#8A5A00"}},
	{Name: "Mawar", Colors: []string{"#3A1722", "#FBF3EF", "#F2DCD6", "#E58A8A", "#A23B4C"}},
}

var fallbackPalette = flattenPalettes(fallbackPalettes)

func flattenPalettes(palettes []paletteSet) []string {
	var colors []string
	for _, palette := range palettes {
		for _, color := range palette.Colors {
			colors = append(colors, strings.ToLower(color))
		}
	}
	return colors
}

func fallbackPaletteFor(color string) (paletteSet, bool) {
	color = strings.ToLower(color)
	for _, palette := range fallbackPalettes {
		for _, candidate := range palette.Colors {
			if strings.ToLower(candidate) == color {
				return palette, true
			}
		}
	}
	return paletteSet{}, false
}

func allowedPalette(facts BriefFacts) map[string]bool {
	allowed := make(map[string]bool)
	if facts.HasPalette {
		for _, color := range facts.Palette {
			allowed[strings.ToLower(color)] = true
		}
		return allowed
	}
	for _, color := range fallbackPalette {
		allowed[strings.ToLower(color)] = true
	}
	return allowed
}
