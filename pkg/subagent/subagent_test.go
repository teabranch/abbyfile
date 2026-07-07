package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

func TestGenerate_WritesSubagentMarkdown(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name:        "reviewer",
		Description: "Reviews Go code",
		Tools:       []string{"Read", "Grep"},
		PromptBody:  "You review Go code carefully.",
	}
	path, err := Generate(def, GenerateConfig{OutputDir: dir, Model: "opus"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := filepath.Join(dir, ".claude", "agents", "reviewer.md")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, sub := range []string{
		"name: reviewer",
		"description: Reviews Go code",
		"model: opus",
		"You review Go code carefully.",
		"## Return Protocol",
		"isolated context window",
		"25-line",
	} {
		if !strings.Contains(s, sub) {
			t.Fatalf("output missing %q:\n%s", sub, s)
		}
	}
}

func TestSummaryLines_DefaultAndOverride(t *testing.T) {
	if got := summaryLines(&definition.AgentDef{}); got != 25 {
		t.Fatalf("default summaryLines = %d, want 25", got)
	}
	def := &definition.AgentDef{ContextBudget: &definition.ContextBudgetDef{SummaryLines: 10}}
	if got := summaryLines(def); got != 10 {
		t.Fatalf("override summaryLines = %d, want 10", got)
	}
}
