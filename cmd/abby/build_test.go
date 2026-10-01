package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAgentOwnTools_DropsBuiltins(t *testing.T) {
	m := &agentManifest{Tools: []manifestTool{
		{Name: "read_file"}, {Name: "run_command"}, {Name: "lint"}, {Name: "memory_read"},
	}}
	got := agentOwnTools(m)
	want := []string{"lint", "memory_read"}
	if !slices.Equal(got, want) {
		t.Fatalf("agentOwnTools = %v, want %v", got, want)
	}
}

func TestAgentOwnTools_NilManifest(t *testing.T) {
	if got := agentOwnTools(nil); got != nil {
		t.Fatalf("agentOwnTools(nil) = %v, want nil", got)
	}
}

// writeFileOnlyProject lays out an Abbyfile whose one agent is binary: false.
func writeFileOnlyProject(t *testing.T, agentMD string) string {
	t.Helper()
	dir := t.TempDir()
	abbyfile := "version: \"1\"\nagents:\n  helper:\n    path: helper.md\n    version: 0.3.0\n    binary: false\n"
	if err := os.WriteFile(filepath.Join(dir, "Abbyfile"), []byte(abbyfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.md"), []byte(agentMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const fileOnlyAgentMD = "---\nname: helper\ndescription: Helps\nabbyfile:\n  tools: [Read, Bash]\n---\n\nYou help.\n"

func TestRunBuild_FileOnlyAgentEmitsSubagentWithoutBinary(t *testing.T) {
	dir := writeFileOnlyProject(t, fileOnlyAgentMD)
	t.Chdir(dir)
	out := filepath.Join(dir, "build")

	// No --subagent flag: a binary: false agent always gets its file.
	if err := runBuild("Abbyfile", out, "", false, false, 0, "claude-code", "", "file", false); err != nil {
		t.Fatalf("runBuild: %v", err)
	}
	md, err := os.ReadFile(filepath.Join(out, ".claude", "agents", "helper.md"))
	if err != nil {
		t.Fatalf("sub-agent file not written: %v", err)
	}
	if !strings.Contains(string(md), "tools: Read, Bash\n") {
		t.Fatalf("expected native tools only:\n%s", md)
	}
	if _, err := os.Stat(filepath.Join(out, "helper")); !os.IsNotExist(err) {
		t.Fatalf("binary should not exist, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf(".mcp.json should not be written for a binary: false agent, stat err = %v", err)
	}
}

func TestRunBuild_FileOnlyAgentRejectsPluginAndCustomTools(t *testing.T) {
	dir := writeFileOnlyProject(t, fileOnlyAgentMD)
	t.Chdir(dir)
	err := runBuild("Abbyfile", "build", "", true, false, 0, "claude-code", "", "file", false)
	if err == nil || !strings.Contains(err.Error(), "--plugin") {
		t.Fatalf("err = %v, want a --plugin error", err)
	}

	withTools := "---\nname: helper\nabbyfile:\n  custom_tools:\n    - name: lint\n      command: echo\n---\n\nx\n"
	t.Chdir(writeFileOnlyProject(t, withTools))
	err = runBuild("Abbyfile", "build", "", false, false, 0, "claude-code", "", "file", false)
	if err == nil || !strings.Contains(err.Error(), "custom_tools") {
		t.Fatalf("err = %v, want a custom_tools error", err)
	}
}
