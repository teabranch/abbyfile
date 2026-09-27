package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type memorySink struct {
	agentName string
	set       func(key, value string) error
}

// NewMemorySink returns a SpillSink that writes overflow into agent memory.
func NewMemorySink(agentName string, set func(key, value string) error) SpillSink {
	return &memorySink{agentName: agentName, set: set}
}

func (s *memorySink) Put(key, value string) (string, error) {
	// Memory keys are flat file names (memory.validateKey rejects path
	// separators), so flatten the shaper's "spill/<tool>" key.
	flat := strings.NewReplacer("/", "-", `\`, "-").Replace(key)
	fullKey := flat + "-" + shortHash(value)
	if err := s.set(fullKey, value); err != nil {
		return "", fmt.Errorf("spill to memory: %w", err)
	}
	return fmt.Sprintf("memory://%s/%s", s.agentName, fullKey), nil
}

type tempFileSink struct {
	agentName string
}

// NewTempFileSink returns a SpillSink that writes overflow under
// ~/.abbyfile/<name>/spill/ and returns a file:// URI.
func NewTempFileSink(agentName string) SpillSink {
	return &tempFileSink{agentName: agentName}
}

func (s *tempFileSink) Put(key, value string) (string, error) {
	dir := SpillDir(s.agentName)
	if dir == "" {
		return "", fmt.Errorf("resolving home directory for spill")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating spill dir: %w", err)
	}
	name := filepath.Base(key) + "-" + shortHash(value) + ".txt"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", fmt.Errorf("writing spill file: %w", err)
	}
	return "file://" + path, nil
}

// SpillDir is where the temp-file sink writes overflow for agentName, or ""
// if the home directory cannot be resolved. The agent passes it to the
// sandbox as a read-only root so the model can read spilled output.
func SpillDir(agentName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".abbyfile", filepath.Base(agentName), "spill")
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
