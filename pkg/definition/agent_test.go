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
	if def.ContextBudget.MaxOutputLines == nil || *def.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("MaxOutputLines = %v, want 500", def.ContextBudget.MaxOutputLines)
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
	if def.ContextBudget.MaxOutputLines == nil || *def.ContextBudget.MaxOutputLines != 500 {
		t.Fatalf("MaxOutputLines = %v, want 500", def.ContextBudget.MaxOutputLines)
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

// TestParseAgentMD_ContextBudget_ZeroMeansUnlimited locks Bug I2's fix:
// an explicit `max_output_lines: 0` in frontmatter must parse to a non-nil
// pointer to 0 (meaning "unlimited"), distinguishable from an omitted
// field (nil, meaning "use the shipped default"). Covers both the dual
// and single frontmatter formats.
func TestParseAgentMD_ContextBudget_ZeroMeansUnlimited(t *testing.T) {
	dualMD := `---
name: my-agent
memory: project
---

---
description: "test"
tools: Read
context_budget:
  max_output_lines: 0
---

Body.`
	def, err := ParseAgentMD(writeTempAgent(t, dualMD))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if def.ContextBudget == nil {
		t.Fatal("ContextBudget is nil")
	}
	if def.ContextBudget.MaxOutputLines == nil {
		t.Fatal("MaxOutputLines is nil, want non-nil pointer to 0 (unlimited)")
	}
	if *def.ContextBudget.MaxOutputLines != 0 {
		t.Fatalf("MaxOutputLines = %d, want 0", *def.ContextBudget.MaxOutputLines)
	}

	singleMD := `---
name: my-agent
description: "test"
abbyfile:
  tools: [Read]
  context_budget:
    max_output_lines: 0
    max_output_bytes: 0
---

Body.`
	def2, err := ParseAgentMD(writeTempAgent(t, singleMD))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if def2.ContextBudget == nil {
		t.Fatal("ContextBudget is nil")
	}
	if def2.ContextBudget.MaxOutputLines == nil || *def2.ContextBudget.MaxOutputLines != 0 {
		t.Fatalf("MaxOutputLines = %v, want non-nil 0", def2.ContextBudget.MaxOutputLines)
	}
	if def2.ContextBudget.MaxOutputBytes == nil || *def2.ContextBudget.MaxOutputBytes != 0 {
		t.Fatalf("MaxOutputBytes = %v, want non-nil 0", def2.ContextBudget.MaxOutputBytes)
	}

	// Omitted (no context_budget numeric fields at all) must stay nil.
	omittedMD := `---
name: my-agent
memory: project
---

---
description: "test"
tools: Read
context_budget:
  on_overflow: spill
---

Body.`
	def3, err := ParseAgentMD(writeTempAgent(t, omittedMD))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if def3.ContextBudget.MaxOutputLines != nil {
		t.Fatalf("MaxOutputLines = %v, want nil (omitted)", def3.ContextBudget.MaxOutputLines)
	}
}
