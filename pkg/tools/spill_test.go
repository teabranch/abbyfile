package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemorySink_Put(t *testing.T) {
	stored := map[string]string{}
	var capturedKey string
	sink := NewMemorySink("my-agent", func(k, v string) error {
		capturedKey = k
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
	if got := stored[capturedKey]; got != "big output" {
		t.Fatalf("stored value mismatch for key %q: got %q, want %q", capturedKey, got, "big output")
	}
	if !strings.HasSuffix(uri, capturedKey) {
		t.Fatalf("URI %q does not end with stored key %q", uri, capturedKey)
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

func TestTempFileSink_SanitizesAgentName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sink := NewTempFileSink("../evil")
	uri, err := sink.Put("spill/run_command", "big output")
	if err != nil {
		t.Fatalf("Put error: %v", err)
	}
	path := strings.TrimPrefix(uri, "file://")
	if strings.Contains(path, "..") {
		t.Fatalf("spill path escapes agent dir: %q", path)
	}
	if !strings.HasPrefix(path, filepath.Join(dir, ".abbyfile")) {
		t.Fatalf("spill path not under ~/.abbyfile: %q", path)
	}
}
