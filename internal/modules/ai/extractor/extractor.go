package extractor

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SupportedExtensions lists the allowed document file extensions.
var SupportedExtensions = map[string]string{
	".pdf":  "pdf",
	".docx": "docx",
	".txt":  "txt",
}

// IsSupported checks if a file name or extension is supported for text extraction.
func IsSupported(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	_, ok := SupportedExtensions[ext]
	return ok
}

// NormalizeType returns the canonical file type string (e.g., "pdf", "docx", "txt").
func NormalizeType(fileNameOrType string) string {
	lower := strings.ToLower(strings.TrimSpace(fileNameOrType))
	if ext := filepath.Ext(lower); ext != "" {
		if t, ok := SupportedExtensions[ext]; ok {
			return t
		}
		return strings.TrimPrefix(ext, ".")
	}
	lower = strings.TrimPrefix(lower, ".")
	switch lower {
	case "pdf", "application/pdf":
		return "pdf"
	case "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case "txt", "text/plain":
		return "txt"
	default:
		return lower
	}
}


// ExtractText extracts plain text content from a supported file on disk.
func ExtractText(filePath string, fileType string) (string, error) {
	norm := NormalizeType(fileType)
	if norm == "" {
		norm = NormalizeType(filepath.Ext(filePath))
	}

	switch norm {
	case "pdf":
		return ExtractPDF(filePath)
	case "docx":
		return ExtractDOCX(filePath)
	case "txt":
		return ExtractTXT(filePath)
	default:
		return "", fmt.Errorf("unsupported file type: %q. Only PDF, DOCX, and TXT are supported", fileType)
	}
}
