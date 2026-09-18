package extractor

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// ExtractPDF extracts plain text from a PDF file on disk.
func ExtractPDF(filePath string) (string, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open pdf file: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	totalPages := r.NumPage()

	for pageIndex := 1; pageIndex <= totalPages; pageIndex++ {
		p := r.Page(pageIndex)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			// Skip or log page error, continue with other pages
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(trimmed)
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("pdf contains no extractable text")
	}

	return result, nil
}
