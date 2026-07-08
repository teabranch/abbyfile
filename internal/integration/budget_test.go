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

// TestIntegration_ContextBudgetCapsToolOutput builds a standalone agent whose
// custom tool emits 1000 lines of output, with a context_budget capped at
// 20 lines (head-tail, 5+5). It runs the tool via `run-tool` and asserts the
// real binary's stdout was shaped: the elision marker is present and the
// line count is far below the raw 1000 lines emitted by the tool.
func TestIntegration_ContextBudgetCapsToolOutput(t *testing.T) {
	projectRoot := findProjectRoot()
	tmp := t.TempDir()

	// Agent .md: a custom tool that emits 1000 lines, and a context_budget
	// capped well below that.
	agentMD := `---
name: budget-test-agent
---

---
description: "An integration test agent for context budget shaping"
tools: Read
custom_tools:
  - name: gen_lines
    command: sh
    args:
      - "-c"
      - "for i in $(seq 1 1000); do echo line $i; done"
    description: "Emits 1000 lines of output"
    input_schema:
      type: object
      properties: {}
context_budget:
  max_output_lines: 20
  on_overflow: head-tail
  head_lines: 5
  tail_lines: 5
---

You are a test agent built with the Abbyfile framework.
`
	agentDir := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "budget-test-agent.md"), []byte(agentMD), 0o644); err != nil {
		t.Fatal(err)
	}

	abbyfileYAML := `version: "1"
agents:
  budget-test-agent:
    path: agents/budget-test-agent.md
    version: 0.1.0
`
	if err := os.WriteFile(filepath.Join(tmp, "Abbyfile"), []byte(abbyfileYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build the agent via abby build (module-dir so go.mod resolves the local module).
	buildDir := filepath.Join(tmp, "build")
	buildCmd := exec.Command(abbyBin, "build",
		"-f", filepath.Join(tmp, "Abbyfile"),
		"-o", buildDir,
		"--module-dir", projectRoot,
	)
	buildCmd.Dir = projectRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("abby build: %v\n%s", err, out)
	}

	binPath := filepath.Join(buildDir, "budget-test-agent")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	runCmd := exec.CommandContext(ctx, binPath, "run-tool", "gen_lines")
	out, err := runCmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("run-tool gen_lines failed: %v\nstderr: %s", err, ee.Stderr)
		}
		t.Fatalf("run-tool gen_lines failed: %v", err)
	}

	output := string(out)
	if !strings.Contains(output, "elided") {
		t.Errorf("expected shaped output to contain elision marker 'elided', got:\n%s", output)
	}
	lineCount := strings.Count(output, "\n")
	if lineCount >= 100 {
		t.Errorf("expected shaped output to have far fewer than 1000 lines (< 100), got %d lines:\n%s", lineCount, output)
	}
}
