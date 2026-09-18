package extractor

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// ExtractTXT reads and extracts text from a plain text (.txt) file on disk.
func ExtractTXT(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read text file: %w", err)
	}

	if !utf8.Valid(data) {
		return "", fmt.Errorf("text file contains invalid non-UTF-8 characters")
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", fmt.Errorf("text file is empty")
	}

	return trimmed, nil
}
