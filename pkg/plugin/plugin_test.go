package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

func TestGenerate_DirectoryStructure(t *testing.T) {
	tmp := t.TempDir()

	// Create a fake binary.
	binaryPath := filepath.Join(tmp, "my-agent")
	os.WriteFile(binaryPath, []byte("#!/bin/sh\necho hello"), 0o755)

	def := &definition.AgentDef{
		Name:        "my-agent",
		Version:     "1.0.0",
		Description: "A test agent",
	}

	skills := []SkillFile{
		{
			Name:        "review-pr",
			Description: "Review a pull request",
			Content:     "Review the PR for quality.",
		},
		{
			Name:        "write-tests",
			Description: "Generate unit tests",
			Content:     "Write tests for the code.",
		},
	}

	outputDir := filepath.Join(tmp, "build")
	os.MkdirAll(outputDir, 0o755)

	err := Generate(def, skills, GenerateConfig{
		OutputDir:  outputDir,
		BinaryPath: binaryPath,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	pluginDir := filepath.Join(outputDir, "my-agent.claude-plugin")

	// Verify directory structure.
	paths := []string{
		filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		filepath.Join(pluginDir, ".mcp.json"),
		filepath.Join(pluginDir, "my-agent"),
		filepath.Join(pluginDir, "skills", "review-pr", "SKILL.md"),
		filepath.Join(pluginDir, "skills", "write-tests", "SKILL.md"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestGenerate_PluginJSON(t *testing.T) {
	tmp := t.TempDir()

	binaryPath := filepath.Join(tmp, "my-agent")
	os.WriteFile(binaryPath, []byte("binary"), 0o755)

	def := &definition.AgentDef{
		Name:        "my-agent",
		Version:     "2.0.0",
		Description: "Test description",
	}

	outputDir := filepath.Join(tmp, "build")
	os.MkdirAll(outputDir, 0o755)

	if err := Generate(def, nil, GenerateConfig{
		OutputDir:  outputDir,
		BinaryPath: binaryPath,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "my-agent.claude-plugin", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}

	var pj pluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}

	if pj.Name != "my-agent" {
		t.Errorf("name = %q, want %q", pj.Name, "my-agent")
	}
	if pj.Version != "2.0.0" {
		t.Errorf("version = %q, want %q", pj.Version, "2.0.0")
	}
	if pj.Description != "Test description" {
		t.Errorf("description = %q, want %q", pj.Description, "Test description")
	}
	if !pj.Abbyfile {
		t.Error("abbyfile = false, want true")
	}
}

func TestGenerate_MCPJSON(t *testing.T) {
	tmp := t.TempDir()

	binaryPath := filepath.Join(tmp, "my-agent")
	os.WriteFile(binaryPath, []byte("binary"), 0o755)

	def := &definition.AgentDef{
		Name:    "my-agent",
		Version: "1.0.0",
	}

	outputDir := filepath.Join(tmp, "build")
	os.MkdirAll(outputDir, 0o755)

	if err := Generate(def, nil, GenerateConfig{
		OutputDir:  outputDir,
		BinaryPath: binaryPath,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "my-agent.claude-plugin", ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}

	var mj mcpJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		t.Fatalf("parsing .mcp.json: %v", err)
	}

	entry, ok := mj.MCPServers["my-agent"]
	if !ok {
		t.Fatal("missing my-agent in mcpServers")
	}
	if entry.Command != "./my-agent" {
		t.Errorf("command = %q, want %q", entry.Command, "./my-agent")
	}
	if len(entry.Args) != 1 || entry.Args[0] != "serve-mcp" {
		t.Errorf("args = %v, want [serve-mcp]", entry.Args)
	}
}

func TestGenerate_SkillContent(t *testing.T) {
	tmp := t.TempDir()

	binaryPath := filepath.Join(tmp, "my-agent")
	os.WriteFile(binaryPath, []byte("binary"), 0o755)

	def := &definition.AgentDef{
		Name:    "my-agent",
		Version: "1.0.0",
	}

	skills := []SkillFile{
		{
			Name:        "review-pr",
			Description: "Review a pull request",
			Content:     "Check code quality and style.",
		},
	}

	outputDir := filepath.Join(tmp, "build")
	os.MkdirAll(outputDir, 0o755)

	if err := Generate(def, skills, GenerateConfig{
		OutputDir:  outputDir,
		BinaryPath: binaryPath,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "my-agent.claude-plugin", "skills", "review-pr", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}

	content := string(data)
	if !contains(content, "name: review-pr") {
		t.Errorf("SKILL.md missing name frontmatter, got:\n%s", content)
	}
	if !contains(content, "description: Review a pull request") {
		t.Errorf("SKILL.md missing description frontmatter, got:\n%s", content)
	}
	if !contains(content, "Check code quality and style.") {
		t.Errorf("SKILL.md missing content body, got:\n%s", content)
	}
}

func TestGenerate_NoSkills(t *testing.T) {
	tmp := t.TempDir()

	binaryPath := filepath.Join(tmp, "my-agent")
	os.WriteFile(binaryPath, []byte("binary"), 0o755)

	def := &definition.AgentDef{
		Name:    "my-agent",
		Version: "1.0.0",
	}

	outputDir := filepath.Join(tmp, "build")
	os.MkdirAll(outputDir, 0o755)

	if err := Generate(def, nil, GenerateConfig{
		OutputDir:  outputDir,
		BinaryPath: binaryPath,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	pluginDir := filepath.Join(outputDir, "my-agent.claude-plugin")

	// Plugin dir should exist but no skills directory.
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("plugin.json should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginDir, "skills")); err == nil {
		t.Error("skills/ directory should not exist when no skills defined")
	}
}

func TestGenerate_BinaryIsExecutable(t *testing.T) {
	tmp := t.TempDir()

	binaryPath := filepath.Join(tmp, "my-agent")
	os.WriteFile(binaryPath, []byte("binary"), 0o755)

	def := &definition.AgentDef{
		Name:    "my-agent",
		Version: "1.0.0",
	}

	outputDir := filepath.Join(tmp, "build")
	os.MkdirAll(outputDir, 0o755)

	if err := Generate(def, nil, GenerateConfig{
		OutputDir:  outputDir,
		BinaryPath: binaryPath,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	info, err := os.Stat(filepath.Join(outputDir, "my-agent.claude-plugin", "my-agent"))
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("binary is not executable, mode = %v", info.Mode())
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
