package extractor

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTXT(t *testing.T) {
	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "sample.txt")
	content := "Hello, this is a plain text file for RAG extraction test.\nSecond line of content."
	if err := os.WriteFile(txtPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write txt file: %v", err)
	}

	extracted, err := ExtractText(txtPath, "txt")
	if err != nil {
		t.Fatalf("ExtractText(txt) failed: %v", err)
	}
	if !strings.Contains(extracted, "plain text file") {
		t.Errorf("extracted text mismatch: %q", extracted)
	}
}

func TestExtractTXTEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(txtPath, []byte("   \n\t  "), 0644); err != nil {
		t.Fatalf("failed to write empty txt: %v", err)
	}

	_, err := ExtractText(txtPath, "txt")
	if err == nil {
		t.Error("expected error for empty text file")
	}
}

func TestExtractDOCX(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := filepath.Join(tmpDir, "test.docx")

	// Create a real minimal docx zip archive with word/document.xml
	f, err := os.Create(docxPath)
	if err != nil {
		t.Fatalf("failed to create docx: %v", err)
	}
	zw := zip.NewWriter(f)

	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
    <w:body>
        <w:p>
            <w:r>
                <w:t>First paragraph in DOCX document.</w:t>
            </w:r>
        </w:p>
        <w:p>
            <w:r>
                <w:t>Second paragraph with important information.</w:t>
            </w:r>
        </w:p>
    </w:body>
</w:document>`

	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("failed to create word/document.xml in zip: %v", err)
	}
	if _, err := w.Write([]byte(docXML)); err != nil {
		t.Fatalf("failed to write document.xml: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	f.Close()

	extracted, err := ExtractText(docxPath, "docx")
	if err != nil {
		t.Fatalf("ExtractText(docx) failed: %v", err)
	}
	if !strings.Contains(extracted, "First paragraph") || !strings.Contains(extracted, "Second paragraph") {
		t.Errorf("extracted docx text mismatch: %q", extracted)
	}
}

func TestExtractUnsupported(t *testing.T) {
	tmpDir := t.TempDir()
	dummyPath := filepath.Join(tmpDir, "image.png")
	if err := os.WriteFile(dummyPath, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := ExtractText(dummyPath, "png")
	if err == nil {
		t.Error("expected error for unsupported file type png")
	}
}

func TestNormalizeType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"report.pdf", "pdf"},
		{".docx", "docx"},
		{"sample.TXT", "txt"},
		{"application/pdf", "pdf"},
		{"unknown.bin", "bin"},
	}

	for _, c := range cases {
		got := NormalizeType(c.in)
		if got != c.want {
			t.Errorf("NormalizeType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
