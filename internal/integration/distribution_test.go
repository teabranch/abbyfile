//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestList verifies the list command works (initially empty, then after install).
func TestList(t *testing.T) {
	tmpHome := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List should show "No agents installed." when registry is empty.
	cmd := exec.CommandContext(ctx, agentfileBin, "list")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("list failed: %v\nstderr: %s", err, string(ee.Stderr))
		}
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(string(out), "No agents installed") {
		t.Errorf("expected 'No agents installed', got %q", string(out))
	}
}

// TestInstallLocalWithRegistry verifies that local install tracks in registry
// and list shows it.
func TestInstallLocalWithRegistry(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a fake build/ directory with the test binary.
	buildDir := filepath.Join(tmpDir, "build")
	os.MkdirAll(buildDir, 0o755)
	// Copy the test binary to build/test-agent.
	cpCmd := exec.Command("cp", binaryPath, filepath.Join(buildDir, "test-agent"))
	if err := cpCmd.Run(); err != nil {
		t.Fatalf("copying test binary: %v", err)
	}

	// Run install from the tmpDir (it looks for build/<name>).
	cmd := exec.CommandContext(ctx, agentfileBin, "install", "test-agent")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Installed test-agent") {
		t.Errorf("expected install confirmation, got %q", string(out))
	}

	// List should now show the agent.
	cmd = exec.CommandContext(ctx, agentfileBin, "list")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	out, err = cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("list failed: %v\nstderr: %s", err, string(ee.Stderr))
		}
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(string(out), "test-agent") {
		t.Errorf("expected test-agent in list, got %q", string(out))
	}
	if !strings.Contains(string(out), "local") {
		t.Errorf("expected 'local' source in list, got %q", string(out))
	}

	// Verify registry file was created.
	regPath := filepath.Join(tmpHome, ".agentfile", "registry.json")
	regData, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("reading registry: %v", err)
	}
	var reg struct {
		Agents map[string]struct {
			Name    string `json:"name"`
			Source  string `json:"source"`
			Version string `json:"version"`
			Scope   string `json:"scope"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(regData, &reg); err != nil {
		t.Fatalf("parsing registry: %v", err)
	}
	agent, ok := reg.Agents["test-agent"]
	if !ok {
		t.Fatal("test-agent not found in registry")
	}
	if agent.Source != "local" {
		t.Errorf("source = %q, want %q", agent.Source, "local")
	}
	if agent.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", agent.Version, "0.1.0")
	}
}

// TestUninstall verifies uninstall removes binary, mcp entry, and registry entry.
func TestUninstall(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Install first.
	buildDir := filepath.Join(tmpDir, "build")
	os.MkdirAll(buildDir, 0o755)
	cpCmd := exec.Command("cp", binaryPath, filepath.Join(buildDir, "test-agent"))
	if err := cpCmd.Run(); err != nil {
		t.Fatalf("copying test binary: %v", err)
	}

	cmd := exec.CommandContext(ctx, agentfileBin, "install", "test-agent")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	// Uninstall.
	cmd = exec.CommandContext(ctx, agentfileBin, "uninstall", "test-agent")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("uninstall failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Uninstalled test-agent") {
		t.Errorf("expected uninstall confirmation, got %q", string(out))
	}

	// Binary should be gone.
	binPath := filepath.Join(tmpDir, ".agentfile", "bin", "test-agent")
	if _, err := os.Stat(binPath); !os.IsNotExist(err) {
		t.Error("binary should be removed after uninstall")
	}

	// List should be empty.
	cmd = exec.CommandContext(ctx, agentfileBin, "list")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	out, err = cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("list after uninstall: %v\nstderr: %s", err, string(ee.Stderr))
		}
		t.Fatalf("list after uninstall: %v", err)
	}
	if !strings.Contains(string(out), "No agents installed") {
		t.Errorf("expected empty list after uninstall, got %q", string(out))
	}
}

// TestPublishDryRun verifies publish --dry-run cross-compiles without creating a release.
func TestPublishDryRun(t *testing.T) {
	projectRoot := findProjectRoot()
	tmpDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Write a minimal agent .md.
	agentDir := filepath.Join(tmpDir, "agents")
	os.MkdirAll(agentDir, 0o755)
	agentMD := `---
name: pub-test
---

---
description: "A publish test agent"
tools: Read
---

You are a test agent.
`
	os.WriteFile(filepath.Join(agentDir, "pub-test.md"), []byte(agentMD), 0o644)

	agentfile := `version: "1"
agents:
  pub-test:
    path: agents/pub-test.md
    version: 0.1.0
`
	os.WriteFile(filepath.Join(tmpDir, "Agentfile"), []byte(agentfile), 0o644)

	cmd := exec.CommandContext(ctx, agentfileBin, "publish", "--dry-run",
		"-f", filepath.Join(tmpDir, "Agentfile"),
	)
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("publish --dry-run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Dry run") {
		t.Errorf("expected dry run message, got %q", string(out))
	}

	// Verify binaries were created for all targets.
	publishDir := filepath.Join(projectRoot, "build", "publish")
	defer os.RemoveAll(publishDir)

	for _, target := range []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"} {
		p := filepath.Join(publishDir, "pub-test-"+target)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected binary %s: %v", p, err)
		}
	}
}
