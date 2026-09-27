package definition

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidateCustomToolsNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 129)
	err := validateCustomTools([]CustomToolDef{{Name: long, Command: "echo"}})
	if err == nil || !strings.Contains(err.Error(), "1-128") {
		t.Fatalf("err = %v, want 1-128 char limit error", err)
	}
}

func TestValidateCustomToolsAcceptsSEP986Names(t *testing.T) {
	cases := []struct {
		name string
	}{
		{"my.tool"},
		{"_tool"},
		{"a-b.c_d"},
	}
	for _, c := range cases {
		err := validateCustomTools([]CustomToolDef{{Name: c.name, Command: "echo"}})
		if err != nil {
			t.Errorf("validateCustomTools(%q) err = %v, want nil", c.name, err)
		}
	}
}

func TestValidateCustomToolsRejectsSpace(t *testing.T) {
	err := validateCustomTools([]CustomToolDef{{Name: "has space", Command: "echo"}})
	if err == nil || !strings.Contains(err.Error(), "custom_tools[0]") {
		t.Fatalf("err = %v, want custom_tools[0] in error", err)
	}
}

func inlineLargeMD(budgetYAML string) string {
	return "---\nname: my-agent\n---\n\n---\ndescription: \"test\"\ntools: Read, Bash\ncontext_budget:\n" + budgetYAML + "---\n\nBody."
}

func TestParseAgentMD_InlineLarge_PerTool(t *testing.T) {
	def, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD("  per_tool:\n    run_command:\n      inline_large: true\n      max_output_bytes: 300000\n")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !def.ContextBudget.PerTool["run_command"].InlineLarge {
		t.Fatalf("inline_large not parsed: %+v", def.ContextBudget.PerTool)
	}
}

func TestParseAgentMD_InlineLarge_InheritsDefaultCap(t *testing.T) {
	// No max_output_bytes anywhere → effective cap is the 262144 default: valid.
	if _, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD("  per_tool:\n    run_command:\n      inline_large: true\n"))); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestParseAgentMD_InlineLarge_BaseLevelRejected(t *testing.T) { // Review Focus #5
	_, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD("  inline_large: true\n")))
	if err == nil || !strings.Contains(err.Error(), "per_tool") {
		t.Fatalf("want error pointing at per_tool, got %v", err)
	}
}

func TestParseAgentMD_InlineLarge_CapBounds(t *testing.T) { // Review Focus #4
	cases := map[string]string{
		"per-tool above ceiling": "  per_tool:\n    run_command:\n      inline_large: true\n      max_output_bytes: 600000\n",
		"inherited unlimited":    "  max_output_bytes: 0\n  per_tool:\n    run_command:\n      inline_large: true\n",
		"inherited above":        "  max_output_bytes: 900000\n  per_tool:\n    run_command:\n      inline_large: true\n",
	}
	for name, yml := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAgentMD(writeTempAgent(t, inlineLargeMD(yml)))
			if err == nil || !strings.Contains(err.Error(), "run_command") || !strings.Contains(err.Error(), "500000") {
				t.Fatalf("want error naming run_command and 500000, got %v", err)
			}
		})
	}
}
