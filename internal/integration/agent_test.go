//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	binaryPath   string
	agentfileBin string
)

func TestMain(m *testing.M) {
	projectRoot := findProjectRoot()

	// Create a temp dir for the entire integration test.
	tmp, err := os.MkdirTemp("", "agentfile-integration-*")
	if err != nil {
		panic("creating temp dir: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	// Build the agentfile CLI into temp dir (avoids macOS case-insensitive
	// conflict with Agentfile YAML at project root).
	agentfileBin = filepath.Join(tmp, "agentfile-cli")
	cmd := exec.Command("go", "build", "-o", agentfileBin, "./cmd/agentfile")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("building agentfile CLI: " + err.Error())
	}

	// Write a minimal agent .md with dual frontmatter.
	agentMD := `---
name: test-agent
memory: project
---

---
description: "An integration test agent"
tools: Read
custom_tools:
  - name: echo_stdin
    command: cat
    description: "Echoes JSON input back via stdin"
    input_schema:
      type: object
      properties:
        message:
          type: string
          description: "Message to echo"
      required: [message]
---

You are a test agent built with the Agentfile framework.
`
	agentDir := filepath.Join(tmp, "agents")
	os.MkdirAll(agentDir, 0o755)
	os.WriteFile(filepath.Join(agentDir, "test-agent.md"), []byte(agentMD), 0o644)

	// Write an Agentfile.
	agentfile := `version: "1"
agents:
  test-agent:
    path: agents/test-agent.md
    version: 0.1.0
`
	os.WriteFile(filepath.Join(tmp, "Agentfile"), []byte(agentfile), 0o644)

	// Build the test agent via agentfile build.
	buildDir := filepath.Join(tmp, "build")
	cmd = exec.Command(agentfileBin, "build", "-f", filepath.Join(tmp, "Agentfile"), "-o", buildDir)
	cmd.Dir = projectRoot // CWD must be project root so DetectModuleDir() finds the local module
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("agentfile build: " + err.Error())
	}

	binaryPath = filepath.Join(buildDir, "test-agent")

	os.Exit(m.Run())
}

// findProjectRoot walks up from the current directory to find go.mod.
func findProjectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// runAgentStdout runs the agent and returns only stdout (logs go to stderr).
func runAgentStdout(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	out, err := cmd.Output() // stdout only; stderr (logs) is ignored
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("agent %v failed: %v\nstderr: %s", args, err, string(ee.Stderr))
		}
		t.Fatalf("agent %v failed: %v", args, err)
	}
	return string(out)
}

func TestVersion(t *testing.T) {
	out := runAgentStdout(t, "--version")
	if !strings.Contains(out, "test-agent v0.1.0") {
		t.Errorf("--version output = %q, want to contain 'test-agent v0.1.0'", out)
	}
}

func TestCustomInstructions(t *testing.T) {
	out := runAgentStdout(t, "--custom-instructions")
	if !strings.Contains(out, "Agentfile framework") {
		t.Errorf("--custom-instructions output = %q, want to contain 'Agentfile framework'", out)
	}
}

func TestDescribe(t *testing.T) {
	out := runAgentStdout(t, "--describe")

	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Tools   []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Memory bool `json:"memory"`
	}
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("parsing --describe JSON: %v\noutput: %s", err, out)
	}
	if manifest.Name != "test-agent" {
		t.Errorf("name = %q, want %q", manifest.Name, "test-agent")
	}
	if manifest.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", manifest.Version, "0.1.0")
	}
	if !manifest.Memory {
		t.Error("memory = false, want true")
	}

	// Should have read_file tool + memory tools + custom tool.
	toolNames := make(map[string]bool)
	for _, tool := range manifest.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["read_file"] {
		t.Error("missing 'read_file' tool in manifest")
	}
	if !toolNames["memory_read"] {
		t.Error("missing 'memory_read' tool in manifest")
	}
	if !toolNames["echo_stdin"] {
		t.Error("missing 'echo_stdin' custom tool in manifest")
	}
}

func TestRunTool(t *testing.T) {
	// Create a temp file to read with the read_file tool.
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("hello from integration test"), 0o644)

	out := runAgentStdout(t, "run-tool", "read_file", "--input", `{"path":"`+tmpFile+`"}`)
	if !strings.Contains(out, "hello from integration test") {
		t.Errorf("run-tool read_file output = %q, want to contain test content", out)
	}
}

func TestRunTool_CustomTool(t *testing.T) {
	input := `{"message":"hello from custom tool"}`
	out := runAgentStdout(t, "run-tool", "echo_stdin", "--input", input)

	// cat should echo back the JSON piped via stdin.
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing output as JSON: %v\noutput: %q", err, out)
	}
	if got["message"] != "hello from custom tool" {
		t.Errorf("message = %v, want %q", got["message"], "hello from custom tool")
	}
}

func TestMemoryLifecycle(t *testing.T) {
	// Isolate memory by overriding HOME.
	tmpHome := t.TempDir()
	setEnv := func(cmd *exec.Cmd) {
		cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write
	cmd := exec.CommandContext(ctx, binaryPath, "memory", "write", "test-key", "test-value")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("memory write: %v\n%s", err, out)
	}

	// Read
	cmd = exec.CommandContext(ctx, binaryPath, "memory", "read", "test-key")
	setEnv(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("memory read: %v", err)
	}
	if strings.TrimSpace(string(out)) != "test-value" {
		t.Errorf("memory read = %q, want %q", strings.TrimSpace(string(out)), "test-value")
	}

	// List
	cmd = exec.CommandContext(ctx, binaryPath, "memory", "list")
	setEnv(cmd)
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if !strings.Contains(string(out), "test-key") {
		t.Errorf("memory list = %q, want to contain 'test-key'", string(out))
	}

	// Append (use -- to stop flag parsing)
	cmd = exec.CommandContext(ctx, binaryPath, "memory", "append", "test-key", "--", "extra")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("memory append: %v\n%s", err, out)
	}

	// Read again to verify append
	cmd = exec.CommandContext(ctx, binaryPath, "memory", "read", "test-key")
	setEnv(cmd)
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("memory read after append: %v", err)
	}
	if strings.TrimSpace(string(out)) != "test-valueextra" {
		t.Errorf("memory read after append = %q, want %q", strings.TrimSpace(string(out)), "test-valueextra")
	}

	// Delete
	cmd = exec.CommandContext(ctx, binaryPath, "memory", "delete", "test-key")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("memory delete: %v\n%s", err, out)
	}

	// Verify deleted
	cmd = exec.CommandContext(ctx, binaryPath, "memory", "read", "test-key")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected error reading deleted key, got: %s", out)
	}
}

func TestServeMCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start the binary with serve-mcp and connect via CommandTransport.
	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "integration-test",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to serve-mcp: %v", err)
	}
	defer session.Close()

	// List tools — should include read_file + memory tools + get_instructions.
	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listResult.Tools) < 2 {
		t.Errorf("expected at least 2 tools, got %d", len(listResult.Tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range listResult.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["read_file"] {
		t.Error("missing 'read_file' tool")
	}
	if !toolNames["get_instructions"] {
		t.Error("missing 'get_instructions' tool")
	}
	if !toolNames["echo_stdin"] {
		t.Error("missing 'echo_stdin' custom tool in MCP listing")
	}
}
