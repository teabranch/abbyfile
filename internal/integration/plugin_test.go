//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlugin(t *testing.T) {
	projectRoot := findProjectRoot()

	tmp := t.TempDir()

	// Write skill files.
	skillsDir := filepath.Join(tmp, "agents", "skills")
	os.MkdirAll(skillsDir, 0o755)
	os.WriteFile(filepath.Join(skillsDir, "review-pr.md"), []byte("Review the PR for code quality and style.\n"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "write-tests.md"), []byte("Generate unit tests for the changed code.\n"), 0o644)

	// Write agent .md with skills.
	agentMD := `---
name: plugin-test-agent
memory: project
---

---
description: "A test agent with skills"
tools: Read
skills:
  - name: review-pr
    description: "Review a pull request for quality"
    path: skills/review-pr.md
  - name: write-tests
    description: "Generate unit tests"
    path: skills/write-tests.md
---

You are a test agent with plugin skills.
`
	os.WriteFile(filepath.Join(tmp, "agents", "plugin-test-agent.md"), []byte(agentMD), 0o644)

	// Write Abbyfile.
	abbyfileYAML := `version: "1"
agents:
  plugin-test-agent:
    path: agents/plugin-test-agent.md
    version: 0.5.0
`
	os.WriteFile(filepath.Join(tmp, "Abbyfile"), []byte(abbyfileYAML), 0o644)

	// Build with --plugin.
	buildDir := filepath.Join(tmp, "build")
	cmd := exec.Command(abbyBin, "build",
		"-f", filepath.Join(tmp, "Abbyfile"),
		"-o", buildDir,
		"--plugin",
	)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("abby build --plugin: %v", err)
	}

	pluginDir := filepath.Join(buildDir, "plugin-test-agent.claude-plugin")

	// Verify plugin directory structure.
	t.Run("directory_structure", func(t *testing.T) {
		paths := []string{
			filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
			filepath.Join(pluginDir, ".mcp.json"),
			filepath.Join(pluginDir, "plugin-test-agent"),
			filepath.Join(pluginDir, "skills", "review-pr", "SKILL.md"),
			filepath.Join(pluginDir, "skills", "write-tests", "SKILL.md"),
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("expected %s to exist: %v", filepath.Base(p), err)
			}
		}
	})

	// Verify plugin.json content.
	t.Run("plugin_json", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
		if err != nil {
			t.Fatalf("reading plugin.json: %v", err)
		}

		var pj struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
			Abbyfile    bool   `json:"abbyfile"`
		}
		if err := json.Unmarshal(data, &pj); err != nil {
			t.Fatalf("parsing plugin.json: %v", err)
		}
		if pj.Name != "plugin-test-agent" {
			t.Errorf("name = %q, want %q", pj.Name, "plugin-test-agent")
		}
		if pj.Version != "0.5.0" {
			t.Errorf("version = %q, want %q", pj.Version, "0.5.0")
		}
		if !pj.Abbyfile {
			t.Error("abbyfile = false, want true")
		}
	})

	// Verify .mcp.json points to binary.
	t.Run("mcp_json", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(pluginDir, ".mcp.json"))
		if err != nil {
			t.Fatalf("reading .mcp.json: %v", err)
		}

		var mj struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &mj); err != nil {
			t.Fatalf("parsing .mcp.json: %v", err)
		}

		entry, ok := mj.MCPServers["plugin-test-agent"]
		if !ok {
			t.Fatal("missing plugin-test-agent in mcpServers")
		}
		if entry.Command != "./plugin-test-agent" {
			t.Errorf("command = %q, want %q", entry.Command, "./plugin-test-agent")
		}
		if len(entry.Args) != 1 || entry.Args[0] != "serve-mcp" {
			t.Errorf("args = %v, want [serve-mcp]", entry.Args)
		}
	})

	// Verify SKILL.md files have frontmatter + content.
	t.Run("skill_content", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(pluginDir, "skills", "review-pr", "SKILL.md"))
		if err != nil {
			t.Fatalf("reading SKILL.md: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "name: review-pr") {
			t.Error("SKILL.md missing name frontmatter")
		}
		if !strings.Contains(content, "description: Review a pull request for quality") {
			t.Error("SKILL.md missing description frontmatter")
		}
		if !strings.Contains(content, "Review the PR for code quality and style.") {
			t.Error("SKILL.md missing content body")
		}
	})

	// Verify binary in plugin dir is executable.
	t.Run("binary_executable", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(pluginDir, "plugin-test-agent"))
		if err != nil {
			t.Fatalf("stat binary: %v", err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("binary is not executable, mode = %v", info.Mode())
		}
	})
}
