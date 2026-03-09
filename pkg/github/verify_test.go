package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	os.WriteFile(path, []byte("hello"), 0o644)

	// Correct hash
	err := VerifyChecksum(path, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wrong hash
	err = VerifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for wrong checksum")
	}
}

func TestParseChecksumFile(t *testing.T) {
	content := `# SHA256 checksums
abc123  agent-darwin-arm64
def456  agent-linux-amd64
`
	sums := ParseChecksumFile(content)
	if len(sums) != 2 {
		t.Fatalf("got %d entries, want 2", len(sums))
	}
	if sums["agent-darwin-arm64"] != "abc123" {
		t.Errorf("darwin hash = %q, want %q", sums["agent-darwin-arm64"], "abc123")
	}
	if sums["agent-linux-amd64"] != "def456" {
		t.Errorf("linux hash = %q, want %q", sums["agent-linux-amd64"], "def456")
	}
}

func TestParseChecksumFile_BinaryMode(t *testing.T) {
	content := "abc123 *agent-binary\n"
	sums := ParseChecksumFile(content)
	if sums["agent-binary"] != "abc123" {
		t.Errorf("got %q, want %q", sums["agent-binary"], "abc123")
	}
}

func TestParseChecksumFile_Empty(t *testing.T) {
	sums := ParseChecksumFile("")
	if len(sums) != 0 {
		t.Errorf("expected empty map, got %d entries", len(sums))
	}
}
