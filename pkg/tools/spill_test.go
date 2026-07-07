package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySink_Put(t *testing.T) {
	stored := map[string]string{}
	sink := NewMemorySink("my-agent", func(k, v string) error {
		stored[k] = v
		return nil
	})
	uri, err := sink.Put("spill/run_command", "big output")
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}
	if !strings.HasPrefix(uri, "memory://my-agent/") {
		t.Fatalf("unexpected URI: %q", uri)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored key, got %d", len(stored))
	}
}

func TestTempFileSink_Put(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sink := NewTempFileSink("my-agent")
	uri, err := sink.Put("spill/run_command", "big output")
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("unexpected URI: %q", uri)
	}
	path := strings.TrimPrefix(uri, "file://")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading spill file: %v", err)
	}
	if string(data) != "big output" {
		t.Fatalf("spill content mismatch: %q", string(data))
	}
	if !strings.Contains(path, filepath.Join(".abbyfile", "my-agent", "spill")) {
		t.Fatalf("spill path not under agent dir: %q", path)
	}
}
