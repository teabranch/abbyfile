package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigGetAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	defaults := CompiledDefaults{
		Model:       "claude-sonnet-4-6",
		ToolTimeout: 30 * time.Second,
	}
	cmd := NewConfigCommand("test-agent", defaults)

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "claude-sonnet-4-6 (compiled)") {
		t.Errorf("expected model compiled default, got: %s", out)
	}
	if !strings.Contains(out, "30s (compiled)") {
		t.Errorf("expected tool_timeout compiled default, got: %s", out)
	}
}

func TestConfigGetAllWithOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Write a config override.
	agentDir := filepath.Join(dir, ".abbyfile", "test-agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("model: gpt-5\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	defaults := CompiledDefaults{
		Model:       "claude-sonnet-4-6",
		ToolTimeout: 30 * time.Second,
	}
	cmd := NewConfigCommand("test-agent", defaults)

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "gpt-5 (override)") {
		t.Errorf("expected model override, got: %s", out)
	}
	if !strings.Contains(out, "30s (compiled)") {
		t.Errorf("expected tool_timeout compiled, got: %s", out)
	}
}

func TestConfigGetField(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	defaults := CompiledDefaults{
		Model:       "opus",
		ToolTimeout: 60 * time.Second,
	}
	cmd := NewConfigCommand("test-agent", defaults)

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get", "model"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "opus (compiled)") {
		t.Errorf("expected 'opus (compiled)', got: %s", out)
	}
}

func TestConfigGetFieldNoModel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	defaults := CompiledDefaults{
		ToolTimeout: 30 * time.Second,
	}
	cmd := NewConfigCommand("test-agent", defaults)

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"get", "model"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "(not set)") {
		t.Errorf("expected '(not set)', got: %s", out)
	}
}

func TestConfigGetUnknownField(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cmd := NewConfigCommand("test-agent", CompiledDefaults{})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestConfigSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cmd := NewConfigCommand("test-agent", CompiledDefaults{})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"set", "model", "gpt-5"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "model = gpt-5") {
		t.Errorf("expected confirmation, got: %s", out)
	}

	// Verify the file was written.
	data, err := os.ReadFile(filepath.Join(dir, ".abbyfile", "test-agent", "config.yaml"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), "gpt-5") {
		t.Errorf("config file does not contain gpt-5: %s", data)
	}
}

func TestConfigReset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Write a config with model override.
	agentDir := filepath.Join(dir, ".abbyfile", "test-agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.yaml"), []byte("model: gpt-5\ntool_timeout: 90s\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewConfigCommand("test-agent", CompiledDefaults{})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"reset", "model"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "model reset to compiled default") {
		t.Errorf("expected reset confirmation, got: %s", out)
	}

	// Verify model was removed but tool_timeout remains.
	data, err := os.ReadFile(filepath.Join(agentDir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if strings.Contains(string(data), "gpt-5") {
		t.Errorf("config still contains model: %s", data)
	}
	if !strings.Contains(string(data), "90s") {
		t.Errorf("config should still contain tool_timeout: %s", data)
	}
}

func TestConfigResetDeletesFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Write a config with only model.
	agentDir := filepath.Join(dir, ".abbyfile", "test-agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(agentDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("model: gpt-5\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewConfigCommand("test-agent", CompiledDefaults{})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"reset", "model"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should be deleted since all fields are nil.
	if _, err := os.Stat(configPath); err == nil {
		t.Error("config file should have been deleted when all fields reset")
	}
}

func TestConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cmd := NewConfigCommand("test-agent", CompiledDefaults{})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"path"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := strings.TrimSpace(buf.String())
	expected := filepath.Join(dir, ".abbyfile", "test-agent", "config.yaml")
	if out != expected {
		t.Errorf("path = %q, want %q", out, expected)
	}
}
