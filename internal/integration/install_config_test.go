//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func abbyIn(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, abbyBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func stageBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "build"), 0o755)
	if out, err := exec.Command("cp", binaryPath, filepath.Join(dir, "build", "test-agent")).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	return dir
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	out, err := abbyIn(t, dir, []string{"HOME=" + home, "ABBY_CONFIG_METHOD=file"}, "install", "--dry-run", "--runtime", "all", "test-agent")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range []string{".mcp.json", ".codex", ".gemini", ".abbyfile"} {
		if _, err := os.Stat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".abbyfile", "registry.json")); !os.IsNotExist(err) {
		t.Error("dry-run touched the registry")
	}
	for _, want := range []string{"RUNTIME", "claude-code", "codex", "gemini", "+ "} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestInstallWithFakeClaudeCLI(t *testing.T) {
	dir := stageBuild(t)
	home := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\nexit 0\n"
	os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755)
	out, err := abbyIn(t, dir, []string{"HOME=" + home, "PATH=" + bin, "ABBY_CONFIG_METHOD=auto"}, "install", "--runtime", "claude-code", "test-agent")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "mcp add-json -s project test-agent") {
		t.Errorf("claude CLI not used: %q\n%s", calls, out)
	}
	if !strings.Contains(out, "cli") {
		t.Errorf("summary must show method cli:\n%s", out)
	}
}
