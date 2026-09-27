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

func buildSandboxAgent(t *testing.T) string {
	t.Helper()
	projectRoot := findProjectRoot()
	tmp := t.TempDir()
	agentMD := `---
name: sandbox-agent
---

---
description: "Sandbox integration agent"
tools: Read, Bash
sandbox:
  allow_commands: ["echo *"]
  max_command_timeout: 5s
---

You test the sandbox.
`
	os.MkdirAll(filepath.Join(tmp, "agents"), 0o755)
	os.WriteFile(filepath.Join(tmp, "agents", "sandbox-agent.md"), []byte(agentMD), 0o644)
	os.WriteFile(filepath.Join(tmp, "Abbyfile"), []byte("version: \"1\"\nagents:\n  sandbox-agent:\n    path: agents/sandbox-agent.md\n    version: 0.1.0\n"), 0o644)
	buildDir := filepath.Join(tmp, "build")
	cmd := exec.Command(abbyBin, "build", "-f", filepath.Join(tmp, "Abbyfile"), "-o", buildDir, "--module-dir", projectRoot)
	cmd.Dir = projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("abby build: %v\n%s", err, out)
	}
	return filepath.Join(buildDir, "sandbox-agent")
}

func runIn(t *testing.T, bin, dir string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestSandboxEndToEnd(t *testing.T) {
	bin := buildSandboxAgent(t)
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, "note.txt"), []byte("inside note"), 0o644)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("SECRET"), 0o644)

	t.Run("allowed command runs", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "run-tool", "run_command", "--input", `{"command":"echo sandboxed"}`)
		if err != nil || !strings.Contains(out, "sandboxed") {
			t.Fatalf("out=%q err=%v stderr=%s", out, err, stderr)
		}
	})
	t.Run("unlisted command refused", func(t *testing.T) {
		_, stderr, err := runIn(t, bin, work, "run-tool", "run_command", "--input", `{"command":"ls"}`)
		if err == nil || !strings.Contains(stderr, "not allowed") {
			t.Fatalf("err=%v stderr=%s", err, stderr)
		}
	})
	t.Run("chaining refused", func(t *testing.T) {
		_, stderr, err := runIn(t, bin, work, "run-tool", "run_command", "--input", `{"command":"echo a; echo b"}`)
		if err == nil || !strings.Contains(stderr, "no shell") {
			t.Fatalf("err=%v stderr=%s", err, stderr)
		}
	})
	t.Run("read inside works", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "run-tool", "read_file", "--input", `{"path":"note.txt"}`)
		if err != nil || !strings.Contains(out, "inside note") {
			t.Fatalf("out=%q err=%v stderr=%s", out, err, stderr)
		}
	})
	t.Run("read outside refused", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "run-tool", "read_file", "--input", `{"path":"`+outside+`"}`)
		if err == nil || strings.Contains(out, "SECRET") || !strings.Contains(stderr, "outside the allowed directories") {
			t.Fatalf("out=%q err=%v stderr=%s", out, err, stderr)
		}
	})
	t.Run("describe reports sandbox", func(t *testing.T) {
		out, stderr, err := runIn(t, bin, work, "--describe")
		if err != nil {
			t.Fatalf("describe: %v\n%s", err, stderr)
		}
		var m struct {
			Sandbox struct {
				Bash              string   `json:"bash"`
				AllowCommands     []string `json:"allowCommands"`
				MaxCommandTimeout string   `json:"maxCommandTimeout"`
				AllowedDirs       []string `json:"allowedDirs"`
			} `json:"sandbox"`
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		}
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("parse: %v\n%s", err, out)
		}
		if m.Sandbox.Bash != "restricted" || len(m.Sandbox.AllowCommands) != 1 || m.Sandbox.AllowCommands[0] != "echo *" || m.Sandbox.MaxCommandTimeout != "5s" {
			t.Fatalf("sandbox = %+v", m.Sandbox)
		}
		for _, tool := range m.Tools {
			if tool.Name == "run_command" && !strings.Contains(tool.Description, "`echo *`") {
				t.Errorf("run_command description not rewritten: %q", tool.Description)
			}
		}
	})
}
