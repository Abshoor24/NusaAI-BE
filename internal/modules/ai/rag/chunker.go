package rag

import (
	"strings"
	"unicode"
)

// DefaultChunkSize is the target maximum size of each text chunk in characters.
const DefaultChunkSize = 800

// DefaultChunkOverlap is the target overlap between adjacent chunks in characters.
const DefaultChunkOverlap = 150

// Chunker segments documents into overlapping text chunks suitable for embedding.
type Chunker struct {
	ChunkSize int
	Overlap   int
}

// NewChunker creates a new Chunker with given chunkSize and overlap.
// If invalid values are provided, sensible defaults are applied.
func NewChunker(chunkSize, overlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = DefaultChunkOverlap
	}
	return &Chunker{
		ChunkSize: chunkSize,
		Overlap:   overlap,
	}
}

// Chunk splits text into a sequence of overlapping strings.
// It trims whitespace and skips empty fragments.
func (c *Chunker) Chunk(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{}
	}

	runes := []rune(text)
	n := len(runes)

	// If text fits in a single chunk, return it immediately.
	if n <= c.ChunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0

	for start < n {
		end := start + c.ChunkSize
		if end >= n {
			chunk := strings.TrimSpace(string(runes[start:]))
			if chunk != "" {
				chunks = append(chunks, chunk)
			}
			break
		}

		// Try to break at a natural boundary (newline, sentence end, or whitespace)
		boundary := findBreakPoint(runes, start, end)
		chunk := strings.TrimSpace(string(runes[start:boundary]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// Move start forward with overlap
		step := (boundary - start) - c.Overlap
		if step <= 0 {
			step = 1 // guarantee forward progress
		}
		start += step
	}

	return chunks
}

func findBreakPoint(runes []rune, start, end int) int {
	// Look back up to 25% of chunk size for natural boundaries
	lookback := (end - start) / 4
	minBreak := end - lookback
	if minBreak < start {
		minBreak = start
	}

	// Priority 1: double newline (paragraph boundary)
	for i := end; i > minBreak; i-- {
		if i > start+1 && runes[i-1] == '\n' && runes[i-2] == '\n' {
			return i
		}
	}

	// Priority 2: single newline
	for i := end; i > minBreak; i-- {
		if i > start && runes[i-1] == '\n' {
			return i
		}
	}

	// Priority 3: sentence boundary (. ! ? followed by space or end)
	for i := end; i > minBreak; i-- {
		if i > start && (runes[i-1] == '.' || runes[i-1] == '!' || runes[i-1] == '?') {
			if i == len(runes) || unicode.IsSpace(runes[i]) {
				return i
			}
		}
	}

	// Priority 4: whitespace
	for i := end; i > minBreak; i-- {
		if i > start && unicode.IsSpace(runes[i-1]) {
			return i
		}
	}

	// Fallback to exact end
	return end
}
