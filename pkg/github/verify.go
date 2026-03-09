package github

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/teabranch/agentfile/pkg/fsutil"
)

// VerifyChecksum computes the SHA256 hash of the file at path and compares it
// to the expected hex string. Returns nil if they match.
func VerifyChecksum(path, expected string) error {
	got, err := fsutil.SHA256File(path)
	if err != nil {
		return fmt.Errorf("computing checksum: %w", err)
	}
	if got != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// ParseChecksumFile parses a SHA256SUMS-style file (hash followed by filename).
// Returns a map of filename → hex hash.
func ParseChecksumFile(content string) map[string]string {
	sums := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: "<hash>  <filename>" or "<hash> <filename>"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hash := parts[0]
			// filename may have a leading * (binary mode indicator)
			name := strings.TrimPrefix(parts[1], "*")
			sums[name] = hash
		}
	}
	return sums
}
