package tools

import (
	"context"
	"strings"
	"testing"
)

func TestLimitedBuffer(t *testing.T) {
	b := NewLimitedBuffer(4)
	n, err := b.Write([]byte("abcdef"))
	if n != 6 || err != nil {
		t.Fatalf("Write = %d, %v; must report full length so the writer never blocks", n, err)
	}
	b.Write([]byte("gh"))
	if !b.Truncated() {
		t.Fatal("Truncated() = false")
	}
	if got, want := b.String(), "abcd\n[output truncated at 4 bytes]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	small := NewLimitedBuffer(100)
	small.Write([]byte("ok"))
	if small.Truncated() || small.String() != "ok" {
		t.Errorf("under-limit buffer = %q truncated=%v", small.String(), small.Truncated())
	}

	def := NewLimitedBuffer(0)
	def.Write([]byte(strings.Repeat("x", 1000)))
	if def.Truncated() {
		t.Error("limit 0 must mean DefaultMaxOutputBytes, not zero")
	}
}

func TestOutputLimitContext(t *testing.T) {
	if got := OutputLimit(context.Background()); got != DefaultMaxOutputBytes {
		t.Errorf("default OutputLimit = %d, want %d", got, DefaultMaxOutputBytes)
	}
	if got := OutputLimit(WithOutputLimit(context.Background(), 42)); got != 42 {
		t.Errorf("OutputLimit = %d, want 42", got)
	}
	if got := OutputLimit(WithOutputLimit(context.Background(), 0)); got != DefaultMaxOutputBytes {
		t.Errorf("non-positive limit must fall back to default, got %d", got)
	}
}
