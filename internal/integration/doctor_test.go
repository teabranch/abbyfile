//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Doctor run from an unrelated directory must still find a local install's entries.
func TestDoctorFromElsewhere(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	env := []string{"HOME=" + home, "ABBY_CONFIG_METHOD=file"}
	if out, err := abbyIn(t, dir, env, "install", "--runtime", "claude-code", "test-agent"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	out, err := abbyIn(t, t.TempDir(), env, "doctor", "--runtime", "claude-code", "test-agent")
	if err != nil || !strings.Contains(out, filepath.Join(dir, ".mcp.json")) {
		t.Fatalf("doctor from elsewhere: %v\n%s", err, out)
	}
}

func TestDoctorAfterInstall(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	env := []string{"HOME=" + home, "ABBY_CONFIG_METHOD=file"}
	if out, err := abbyIn(t, dir, env, "install", "--runtime", "claude-code", "test-agent"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	mcpPath := filepath.Join(dir, ".mcp.json")
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("reading .mcp.json after install: %v", err)
	}
	out, err := abbyIn(t, dir, env, "doctor", "--runtime", "claude-code", "test-agent")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	for _, want := range []string{"test-agent", "2026-07-28", "2025-11-25", ".mcp.json", "✓"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
	// doctor only reads configs; it must never write, back up or modify them.
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("reading .mcp.json after doctor: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf(".mcp.json changed by `abby doctor`:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(mcpPath + ".abbyfile.bak"); !os.IsNotExist(err) {
		t.Errorf("abby doctor must not create a backup file, found %s", mcpPath+".abbyfile.bak")
	}
}
