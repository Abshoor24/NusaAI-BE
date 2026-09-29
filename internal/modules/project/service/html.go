package service

import (
	"strings"

	"github.com/Abshoor24/backend-go/internal/apperror"
)

// CleanHTML strips Markdown code fences and surrounding whitespace from raw
// model output. Models often wrap HTML in ```html ... ``` despite the system
// prompt; the stored version must be the bare document.
func CleanHTML(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		// Drop the opening fence line (``` or ```html).
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		} else {
			s = ""
		}
		// Drop the closing fence and anything after it.
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

// ValidateHTML enforces the api.md/architecture.md shape contract:
// a real HTML document within the byte budget. Callers map failures to a
// generic SSE error event without storing a version.
func ValidateHTML(doc string, maxBytes int64) error {
	if int64(len(doc)) > maxBytes {
		return apperror.AIProvider("Generated website exceeds size limit", nil)
	}
	lower := strings.ToLower(doc)
	hasOpen := strings.Contains(lower, "<!doctype html") || strings.Contains(lower, "<html")
	hasClose := strings.Contains(lower, "</html>")
	if !hasOpen || !hasClose {
		return apperror.AIProvider("Generated output is not a valid HTML document", nil)
	}
	return nil
}

// CleanAndValidate is the pipeline finalization step applied to the full
// streamed buffer before any version is stored.
func CleanAndValidate(raw string, maxBytes int64) (string, error) {
	doc := CleanHTML(raw)
	if err := ValidateHTML(doc, maxBytes); err != nil {
		return "", err
	}
	return doc, nil
}
