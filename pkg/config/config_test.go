package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IsZero() {
		t.Errorf("expected zero config for missing file, got %+v", cfg)
	}
}

func TestLoadFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `model: gpt-5
tool_timeout: 120s
memory_limits:
  max_keys: 500
  max_value_bytes: 1048576
  max_total_bytes: 10485760
  ttl: 72h
command_policy:
  allowed_prefixes:
    - "go "
    - "make "
  denied_substrings:
    - "rm -rf"
  max_output_bytes: 5242880
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model: got %v, want gpt-5", cfg.Model)
	}
	if cfg.ToolTimeout == nil || *cfg.ToolTimeout != "120s" {
		t.Errorf("tool_timeout: got %v, want 120s", cfg.ToolTimeout)
	}

	if cfg.MemoryLimits == nil {
		t.Fatal("memory_limits is nil")
	}
	if cfg.MemoryLimits.MaxKeys == nil || *cfg.MemoryLimits.MaxKeys != 500 {
		t.Errorf("max_keys: got %v, want 500", cfg.MemoryLimits.MaxKeys)
	}
	if cfg.MemoryLimits.MaxValueBytes == nil || *cfg.MemoryLimits.MaxValueBytes != 1048576 {
		t.Errorf("max_value_bytes: got %v, want 1048576", cfg.MemoryLimits.MaxValueBytes)
	}
	if cfg.MemoryLimits.MaxTotalBytes == nil || *cfg.MemoryLimits.MaxTotalBytes != 10485760 {
		t.Errorf("max_total_bytes: got %v, want 10485760", cfg.MemoryLimits.MaxTotalBytes)
	}
	if cfg.MemoryLimits.TTL == nil || *cfg.MemoryLimits.TTL != "72h" {
		t.Errorf("ttl: got %v, want 72h", cfg.MemoryLimits.TTL)
	}

	if cfg.CommandPolicy == nil {
		t.Fatal("command_policy is nil")
	}
	if cfg.CommandPolicy.AllowedPrefixes == nil || len(*cfg.CommandPolicy.AllowedPrefixes) != 2 {
		t.Errorf("allowed_prefixes: got %v, want 2 entries", cfg.CommandPolicy.AllowedPrefixes)
	}
	if cfg.CommandPolicy.DeniedSubstrings == nil || len(*cfg.CommandPolicy.DeniedSubstrings) != 1 {
		t.Errorf("denied_substrings: got %v, want 1 entry", cfg.CommandPolicy.DeniedSubstrings)
	}
	if cfg.CommandPolicy.MaxOutputBytes == nil || *cfg.CommandPolicy.MaxOutputBytes != 5242880 {
		t.Errorf("max_output_bytes: got %v, want 5242880", cfg.CommandPolicy.MaxOutputBytes)
	}
}

func TestLoadPartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("model: claude-sonnet-4-6\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Model == nil || *cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("model: got %v, want claude-sonnet-4-6", cfg.Model)
	}
	if cfg.ToolTimeout != nil {
		t.Errorf("tool_timeout should be nil, got %v", *cfg.ToolTimeout)
	}
	if cfg.MemoryLimits != nil {
		t.Errorf("memory_limits should be nil, got %+v", cfg.MemoryLimits)
	}
	if cfg.CommandPolicy != nil {
		t.Errorf("command_policy should be nil, got %+v", cfg.CommandPolicy)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(":\ninvalid: [yaml: {broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestWriteAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent", "config.yaml")

	model := "o3"
	timeout := "60s"
	maxKeys := 100
	cfg := &Config{
		Model:       &model,
		ToolTimeout: &timeout,
		MemoryLimits: &MemoryLimitsOverride{
			MaxKeys: &maxKeys,
		},
	}

	if err := WriteTo(path, cfg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if got.Model == nil || *got.Model != "o3" {
		t.Errorf("model round-trip: got %v, want o3", got.Model)
	}
	if got.ToolTimeout == nil || *got.ToolTimeout != "60s" {
		t.Errorf("tool_timeout round-trip: got %v, want 60s", got.ToolTimeout)
	}
	if got.MemoryLimits == nil || got.MemoryLimits.MaxKeys == nil || *got.MemoryLimits.MaxKeys != 100 {
		t.Errorf("max_keys round-trip: got %v, want 100", got.MemoryLimits)
	}
	// Unset fields should remain nil.
	if got.CommandPolicy != nil {
		t.Errorf("command_policy should be nil after round-trip")
	}
}

func TestWriteField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write model field to new file.
	if err := WriteFieldTo(path, "model", "gpt-5"); err != nil {
		t.Fatalf("write field error: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model: got %v, want gpt-5", cfg.Model)
	}

	// Write another field — model should be preserved.
	if err := WriteFieldTo(path, "tool_timeout", "90s"); err != nil {
		t.Fatalf("write field error: %v", err)
	}

	cfg, err = LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "gpt-5" {
		t.Errorf("model should be preserved: got %v", cfg.Model)
	}
	if cfg.ToolTimeout == nil || *cfg.ToolTimeout != "90s" {
		t.Errorf("tool_timeout: got %v, want 90s", cfg.ToolTimeout)
	}
}

func TestWriteFieldUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := WriteFieldTo(path, "name", "bad")
	if err == nil {
		t.Fatal("expected error for unsupported field")
	}
}

func TestLoadViaAgentName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Create config at expected path.
	agentDir := filepath.Join(dir, ".agentfile", "test-agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("model: haiku\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model == nil || *cfg.Model != "haiku" {
		t.Errorf("model: got %v, want haiku", cfg.Model)
	}
}

func TestResetField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write config with two fields.
	model := "gpt-5"
	timeout := "90s"
	cfg := &Config{Model: &model, ToolTimeout: &timeout}
	if err := WriteTo(path, cfg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Reset model — tool_timeout should remain.
	if err := ResetFieldTo(path, "model"); err != nil {
		t.Fatalf("reset error: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if got.Model != nil {
		t.Errorf("model should be nil after reset, got %v", *got.Model)
	}
	if got.ToolTimeout == nil || *got.ToolTimeout != "90s" {
		t.Errorf("tool_timeout should be preserved, got %v", got.ToolTimeout)
	}
}

func TestResetFieldDeletesFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write config with only model.
	model := "gpt-5"
	cfg := &Config{Model: &model}
	if err := WriteTo(path, cfg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Reset model — file should be deleted.
	if err := ResetFieldTo(path, "model"); err != nil {
		t.Fatalf("reset error: %v", err)
	}

	if _, err := os.Stat(path); err == nil {
		t.Error("config file should be deleted when all fields are nil")
	}
}

func TestResetFieldUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("model: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ResetFieldTo(path, "name")
	if err == nil {
		t.Fatal("expected error for unsupported field")
	}
}

func TestResetFieldMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.yaml")

	// Resetting a field on a missing file should be a no-op (file already absent).
	err := ResetFieldTo(path, "model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsZero(t *testing.T) {
	if !(&Config{}).IsZero() {
		t.Error("empty config should be zero")
	}
	model := "test"
	if (&Config{Model: &model}).IsZero() {
		t.Error("config with model should not be zero")
	}
}
