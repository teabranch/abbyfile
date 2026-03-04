package definition

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Agentfile")

	content := `version: "1"
agents:
  go-pro:
    path: .claude/agents/go-pro.md
    version: 0.1.0
  tool-eng:
    path: .claude/agents/tool-eng.md
    version: 0.2.0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	af, err := ParseAgentfile(path)
	if err != nil {
		t.Fatalf("ParseAgentfile: %v", err)
	}

	if af.Version != "1" {
		t.Errorf("version = %q, want %q", af.Version, "1")
	}
	if len(af.Agents) != 2 {
		t.Fatalf("agents count = %d, want 2", len(af.Agents))
	}

	gp := af.Agents["go-pro"]
	if gp.Path != ".claude/agents/go-pro.md" {
		t.Errorf("go-pro path = %q", gp.Path)
	}
	if gp.Version != "0.1.0" {
		t.Errorf("go-pro version = %q", gp.Version)
	}

	te := af.Agents["tool-eng"]
	if te.Version != "0.2.0" {
		t.Errorf("tool-eng version = %q", te.Version)
	}
}

func TestParseAgentfile_Validation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "missing version",
			content: "agents:\n  x:\n    path: x.md\n    version: 1.0.0\n",
			wantErr: "version is required",
		},
		{
			name:    "no agents",
			content: "version: \"1\"\n",
			wantErr: "at least one agent",
		},
		{
			name:    "missing path",
			content: "version: \"1\"\nagents:\n  x:\n    version: 1.0.0\n",
			wantErr: "path is required",
		},
		{
			name:    "missing agent version",
			content: "version: \"1\"\nagents:\n  x:\n    path: x.md\n",
			wantErr: "version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "Agentfile")
			os.WriteFile(path, []byte(tt.content), 0o644)

			_, err := ParseAgentfile(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestParseAgentMD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")

	content := `---
name: test-agent
description: "short desc"
model: sonnet
memory: project
---

---
name: test-agent-full
description: "A test agent for unit testing the parser"
tools: Read, Write, Bash, Glob
model: sonnet
---

You are a test agent.

Do test things.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := ParseAgentMD(path)
	if err != nil {
		t.Fatalf("ParseAgentMD: %v", err)
	}

	if def.Name != "test-agent" {
		t.Errorf("name = %q, want %q", def.Name, "test-agent")
	}
	if def.Description != "A test agent for unit testing the parser" {
		t.Errorf("description = %q", def.Description)
	}
	if !def.Memory {
		t.Error("memory = false, want true")
	}

	wantTools := []string{"Read", "Write", "Bash", "Glob"}
	if len(def.Tools) != len(wantTools) {
		t.Fatalf("tools = %v, want %v", def.Tools, wantTools)
	}
	for i, tool := range def.Tools {
		if tool != wantTools[i] {
			t.Errorf("tools[%d] = %q, want %q", i, tool, wantTools[i])
		}
	}

	if !containsStr(def.PromptBody, "You are a test agent.") {
		t.Errorf("prompt body missing expected content, got: %q", def.PromptBody)
	}
	if !containsStr(def.PromptBody, "Do test things.") {
		t.Errorf("prompt body missing 'Do test things.', got: %q", def.PromptBody)
	}
}

func TestParseAgentMD_NoMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")

	content := `---
name: minimal
---

---
tools: Read
---

Minimal prompt.
`
	os.WriteFile(path, []byte(content), 0o644)

	def, err := ParseAgentMD(path)
	if err != nil {
		t.Fatalf("ParseAgentMD: %v", err)
	}

	if def.Memory {
		t.Error("memory = true, want false")
	}
	if len(def.Tools) != 1 || def.Tools[0] != "Read" {
		t.Errorf("tools = %v, want [Read]", def.Tools)
	}
}

func TestParseAgentMD_CustomTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")

	content := `---
name: deploy-agent
---

---
tools: Read, Write
custom_tools:
  - name: deploy
    command: ./scripts/deploy.sh
    description: "Deploy the application"
    args: ["--verbose"]
    input_schema:
      type: object
      properties:
        environment:
          type: string
          description: "Target environment"
      required: [environment]
  - name: healthcheck
    command: curl
    description: "Check service health"
---

Custom tools agent.
`
	os.WriteFile(path, []byte(content), 0o644)

	def, err := ParseAgentMD(path)
	if err != nil {
		t.Fatalf("ParseAgentMD: %v", err)
	}

	// Builtin tools should still be parsed.
	if len(def.Tools) != 2 || def.Tools[0] != "Read" || def.Tools[1] != "Write" {
		t.Errorf("tools = %v, want [Read, Write]", def.Tools)
	}

	// Custom tools.
	if len(def.CustomTools) != 2 {
		t.Fatalf("custom tools count = %d, want 2", len(def.CustomTools))
	}

	deploy := def.CustomTools[0]
	if deploy.Name != "deploy" {
		t.Errorf("custom_tools[0].name = %q, want %q", deploy.Name, "deploy")
	}
	if deploy.Command != "./scripts/deploy.sh" {
		t.Errorf("custom_tools[0].command = %q", deploy.Command)
	}
	if deploy.Description != "Deploy the application" {
		t.Errorf("custom_tools[0].description = %q", deploy.Description)
	}
	if len(deploy.Args) != 1 || deploy.Args[0] != "--verbose" {
		t.Errorf("custom_tools[0].args = %v, want [--verbose]", deploy.Args)
	}
	if deploy.InputSchema == nil {
		t.Fatal("custom_tools[0].input_schema is nil")
	}
	if deploy.InputSchema["type"] != "object" {
		t.Errorf("custom_tools[0].input_schema.type = %v", deploy.InputSchema["type"])
	}

	healthcheck := def.CustomTools[1]
	if healthcheck.Name != "healthcheck" {
		t.Errorf("custom_tools[1].name = %q", healthcheck.Name)
	}
	if healthcheck.InputSchema != nil {
		t.Errorf("custom_tools[1].input_schema should be nil, got %v", healthcheck.InputSchema)
	}
}

func TestParseAgentMD_CustomToolsValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "missing name",
			yaml: `custom_tools:
  - command: ./test.sh
    description: "test"`,
			wantErr: "name is required",
		},
		{
			name: "missing command",
			yaml: `custom_tools:
  - name: test
    description: "test"`,
			wantErr: "command is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "agent.md")
			content := "---\nname: test\n---\n\n---\n" + tt.yaml + "\n---\n\nPrompt.\n"
			os.WriteFile(path, []byte(content), 0o644)

			_, err := ParseAgentMD(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsStr(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseAgentMD_NoCustomTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")

	content := `---
name: simple
---

---
tools: Read
---

Simple prompt.
`
	os.WriteFile(path, []byte(content), 0o644)

	def, err := ParseAgentMD(path)
	if err != nil {
		t.Fatalf("ParseAgentMD: %v", err)
	}
	if len(def.CustomTools) != 0 {
		t.Errorf("custom tools = %v, want empty", def.CustomTools)
	}
}

func TestParseDualFrontmatter_TooFewDelimiters(t *testing.T) {
	_, _, _, err := parseDualFrontmatter("---\nfoo: bar\n---\nno second block")
	if err == nil {
		t.Fatal("expected error for too few delimiters")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
