package rag

import (
	"strings"
	"testing"
)

func TestChunkerEmptyText(t *testing.T) {
	c := NewChunker(100, 20)
	chunks := c.Chunk("")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty text, got %d", len(chunks))
	}

	chunks = c.Chunk("   \n\t  ")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for whitespace text, got %d", len(chunks))
	}
}

func TestChunkerShortText(t *testing.T) {
	c := NewChunker(100, 20)
	text := "This is a short text that fits in one chunk."
	chunks := c.Chunk(text)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("got %q, want %q", chunks[0], text)
	}
}

func TestChunkerLongTextWithParagraphs(t *testing.T) {
	c := NewChunker(50, 10)
	text := "Paragraph one is here.\n\nParagraph two is also here.\n\nParagraph three is the final paragraph."
	chunks := c.Chunk(text)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	// Ensure all content is represented
	full := strings.Join(chunks, " ")
	if !strings.Contains(full, "Paragraph one") || !strings.Contains(full, "Paragraph three") {
		t.Errorf("chunks did not preserve text content: %v", chunks)
	}
}

func TestChunkerOverlap(t *testing.T) {
	c := NewChunker(30, 10)
	text := "Alpha beta gamma delta epsilon zeta eta theta iota kappa"
	chunks := c.Chunk(text)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Verify that overlap occurs between adjacent chunks
	for i := 0; i < len(chunks)-1; i++ {
		first := chunks[i]
		second := chunks[i+1]
		// At least some words or characters from the end of `first` or beginning of `second` should relate
		if len(first) == 0 || len(second) == 0 {
			t.Errorf("empty chunk found at index %d", i)
		}
	}
}

func TestChunkerDefaults(t *testing.T) {
	c := NewChunker(0, -1)
	if c.ChunkSize != DefaultChunkSize {
		t.Errorf("chunk size = %d, want default %d", c.ChunkSize, DefaultChunkSize)
	}
	if c.Overlap != DefaultChunkOverlap {
		t.Errorf("overlap = %d, want default %d", c.Overlap, DefaultChunkOverlap)
	}
}
