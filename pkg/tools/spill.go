package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	fullKey := key + "-" + shortHash(value)
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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home: %w", err)
	}
	dir := filepath.Join(home, ".abbyfile", filepath.Base(s.agentName), "spill")
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

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
