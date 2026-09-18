package extractor

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ExtractDOCX extracts plain text paragraphs from a Microsoft Word (.docx) file on disk.
// DOCX files are standard OpenXML ZIP archives containing 'word/document.xml'.
func ExtractDOCX(filePath string) (string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("open docx archive: %w", err)
	}
	defer zr.Close()

	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}

	if docFile == nil {
		return "", fmt.Errorf("invalid docx file: word/document.xml not found")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", fmt.Errorf("read document.xml: %w", err)
	}
	defer rc.Close()

	return parseDocumentXML(rc)
}

func parseDocumentXML(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var sb strings.Builder
	var currentParagraph strings.Builder
	inText := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("xml decode error: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				currentParagraph.Reset()
			case "t":
				inText = true
			case "br":
				currentParagraph.WriteString("\n")
			case "tab":
				currentParagraph.WriteString("\t")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				pText := strings.TrimSpace(currentParagraph.String())
				if pText != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n\n")
					}
					sb.WriteString(pText)
				}
				currentParagraph.Reset()
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				currentParagraph.Write(t)
			}
		}
	}

	// Flush any trailing paragraph text
	if currentParagraph.Len() > 0 {
		pText := strings.TrimSpace(currentParagraph.String())
		if pText != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(pText)
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("docx contains no extractable text")
	}

	return result, nil
}
