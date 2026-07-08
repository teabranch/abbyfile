package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
	"gopkg.in/yaml.v3"
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

// extractFrontmatter returns the YAML frontmatter block between the first
// two "---" delimiter lines.
func extractFrontmatter(t *testing.T, content string) string {
	t.Helper()
	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 {
		t.Fatalf("could not locate frontmatter delimiters in:\n%s", content)
	}
	return parts[1]
}

func TestGenerate_EscapesSpecialCharsInDescription(t *testing.T) {
	dir := t.TempDir()
	wantDescription := "Reviews Go code: focuses on concurrency"
	def := &definition.AgentDef{
		Name:        "reviewer",
		Description: wantDescription,
		PromptBody:  "You review Go code carefully.",
	}
	path, err := Generate(def, GenerateConfig{OutputDir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fm := extractFrontmatter(t, string(data))

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(fm), &parsed); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v\nfrontmatter:\n%s", err, fm)
	}
	got, ok := parsed["description"].(string)
	if !ok {
		t.Fatalf("description missing or not a string in parsed frontmatter: %#v", parsed)
	}
	if got != wantDescription {
		t.Fatalf("description round-trip = %q, want %q", got, wantDescription)
	}
}

func TestGenerate_EmitsToolsAndReturnProtocolGuidance(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name:       "reviewer",
		Tools:      []string{"Read", "Grep"},
		PromptBody: "You review Go code carefully.",
	}
	path, err := Generate(def, GenerateConfig{OutputDir: dir})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, sub := range []string{
		"tools: Read, Grep",
		"Do NOT paste",
		"memory://",
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
