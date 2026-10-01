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
	elsewhere := t.TempDir()
	out, err := abbyIn(t, elsewhere, env, "doctor", "--runtime", "claude-code", "test-agent")
	if err != nil || !strings.Contains(out, filepath.Join(dir, ".mcp.json")) {
		t.Fatalf("doctor from elsewhere: %v\n%s", err, out)
	}
	// Fix round 1 (controller ruling): --describe and the MCP handshake must
	// run in the agent's own project root (dir, the install directory), not
	// in the directory `abby doctor` was invoked from (elsewhere, above) —
	// otherwise a sandbox with a relative allowedDirs entry (default ["."])
	// would report the wrong directory. Assert the resolved, absolute
	// project directory appears in the sandbox check, and the ad hoc
	// directory doctor ran from does not. dir is resolved through symlinks
	// (as the spawned agent's own os.Getwd() reports it, e.g. macOS
	// /var -> /private/var) to compare like with like.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolving %s: %v", dir, err)
	}
	if !strings.Contains(out, "allowed_dirs="+resolvedDir) {
		t.Errorf("doctor from elsewhere: sandbox allowed_dirs must resolve against the install's project root %q:\n%s", resolvedDir, out)
	}
	if resolvedElsewhere, err := filepath.EvalSymlinks(elsewhere); err == nil && strings.Contains(out, "allowed_dirs="+resolvedElsewhere) {
		t.Errorf("doctor from elsewhere: sandbox allowed_dirs must not be the directory doctor ran from (%s):\n%s", resolvedElsewhere, out)
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
