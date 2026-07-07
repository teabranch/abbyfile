package definition

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempAgent(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "agent.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseAgentMD_ContextBudget_Dual(t *testing.T) {
	md := `---
name: my-agent
memory: project
---

---
description: "test"
tools: Read
context_budget:
  max_output_lines: 500
  on_overflow: spill
  head_lines: 50
  tail_lines: 10
  per_tool:
    run_command:
      on_overflow: head-tail
---

Prompt body.`
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if def.ContextBudget == nil {
		t.Fatal("ContextBudget is nil")
	}
	if def.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("MaxOutputLines = %d, want 500", def.ContextBudget.MaxOutputLines)
	}
	if def.ContextBudget.OnOverflow != "spill" {
		t.Fatalf("OnOverflow = %q, want spill", def.ContextBudget.OnOverflow)
	}
	pt, ok := def.ContextBudget.PerTool["run_command"]
	if !ok || pt.OnOverflow != "head-tail" {
		t.Fatalf("per_tool run_command override missing/wrong: %+v", def.ContextBudget.PerTool)
	}
}

func TestParseAgentMD_ContextBudget_Single(t *testing.T) {
	md := `---
name: my-agent
description: "test"
abbyfile:
  tools: [Read]
  context_budget:
    max_output_lines: 500
    on_overflow: spill
    head_lines: 50
    tail_lines: 10
    per_tool:
      run_command:
        on_overflow: head-tail
---

Prompt body.`
	def, err := ParseAgentMD(writeTempAgent(t, md))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if def.ContextBudget == nil {
		t.Fatal("ContextBudget is nil")
	}
	if def.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("MaxOutputLines = %d, want 500", def.ContextBudget.MaxOutputLines)
	}
	if def.ContextBudget.OnOverflow != "spill" {
		t.Fatalf("OnOverflow = %q, want spill", def.ContextBudget.OnOverflow)
	}
	pt, ok := def.ContextBudget.PerTool["run_command"]
	if !ok || pt.OnOverflow != "head-tail" {
		t.Fatalf("per_tool run_command override missing/wrong: %+v", def.ContextBudget.PerTool)
	}
}

func TestParseAgentMD_ContextBudget_NegativeRejected(t *testing.T) {
	md := `---
name: my-agent
memory: project
---

---
description: "test"
tools: Read
context_budget:
  max_output_lines: -1
---

Body.`
	_, err := ParseAgentMD(writeTempAgent(t, md))
	if err == nil {
		t.Fatal("expected error for negative max_output_lines")
	}
}

func TestParseAgentMD_ContextBudget_InvalidStrategy(t *testing.T) {
	md := `---
name: my-agent
---

---
description: "test"
tools: Read
context_budget:
  on_overflow: bogus
---

Body.`
	_, err := ParseAgentMD(writeTempAgent(t, md))
	if err == nil {
		t.Fatal("expected error for invalid on_overflow")
	}
}
