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

func TestConfigPath(t *testing.T) {
	out := runAgentStdout(t, "config", "path")
	trimmed := strings.TrimSpace(out)
	if !strings.HasSuffix(trimmed, filepath.Join(".agentfile", "test-agent", "config.yaml")) {
		t.Errorf("config path = %q, want to end with .agentfile/test-agent/config.yaml", trimmed)
	}
}

func TestConfigGetDefaults(t *testing.T) {
	tmpHome := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "config", "get")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("config get failed: %v\nstderr: %s", err, ee.Stderr)
		}
		t.Fatalf("config get failed: %v", err)
	}

	output := string(out)
	// Should show compiled defaults.
	if !strings.Contains(output, "model:") {
		t.Errorf("config get output should contain 'model:', got: %s", output)
	}
	if !strings.Contains(output, "tool_timeout:") {
		t.Errorf("config get output should contain 'tool_timeout:', got: %s", output)
	}
	if !strings.Contains(output, "(compiled)") {
		t.Errorf("config get output should show '(compiled)' for defaults, got: %s", output)
	}
}

func TestConfigSetAndGet(t *testing.T) {
	tmpHome := t.TempDir()
	setEnv := func(cmd *exec.Cmd) {
		cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set model override.
	cmd := exec.CommandContext(ctx, binaryPath, "config", "set", "model", "gpt-5")
	setEnv(cmd)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("config set failed: %v\nstderr: %s", err, ee.Stderr)
		}
		t.Fatalf("config set failed: %v", err)
	}
	if !strings.Contains(string(out), "model = gpt-5") {
		t.Errorf("config set output = %q, want to contain 'model = gpt-5'", string(out))
	}

	// Get model — should show override.
	cmd = exec.CommandContext(ctx, binaryPath, "config", "get", "model")
	setEnv(cmd)
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("config get model: %v", err)
	}
	if !strings.Contains(string(out), "gpt-5 (override)") {
		t.Errorf("config get model = %q, want to contain 'gpt-5 (override)'", string(out))
	}

	// Verify --describe reflects the override.
	cmd = exec.CommandContext(ctx, binaryPath, "--describe")
	setEnv(cmd)
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("--describe: %v", err)
	}
	var manifest struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(out, &manifest); err != nil {
		t.Fatalf("parsing --describe: %v\noutput: %s", err, out)
	}
	if manifest.Model != "gpt-5" {
		t.Errorf("--describe model = %q, want %q", manifest.Model, "gpt-5")
	}
}

func TestConfigReset(t *testing.T) {
	tmpHome := t.TempDir()
	setEnv := func(cmd *exec.Cmd) {
		cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set model override.
	cmd := exec.CommandContext(ctx, binaryPath, "config", "set", "model", "gpt-5")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config set: %v\n%s", err, out)
	}

	// Also set tool_timeout so file isn't deleted.
	cmd = exec.CommandContext(ctx, binaryPath, "config", "set", "tool_timeout", "120s")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config set tool_timeout: %v\n%s", err, out)
	}

	// Reset model.
	cmd = exec.CommandContext(ctx, binaryPath, "config", "reset", "model")
	setEnv(cmd)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("config reset: %v\nstderr: %s", err, ee.Stderr)
		}
		t.Fatalf("config reset: %v", err)
	}
	if !strings.Contains(string(out), "model reset to compiled default") {
		t.Errorf("config reset output = %q, want to contain reset message", string(out))
	}

	// Get model — should show compiled default, not override.
	cmd = exec.CommandContext(ctx, binaryPath, "config", "get", "model")
	setEnv(cmd)
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("config get model: %v", err)
	}
	if strings.Contains(string(out), "gpt-5") {
		t.Errorf("config get model after reset should not contain gpt-5, got: %s", out)
	}
	if !strings.Contains(string(out), "(compiled)") {
		t.Errorf("config get model after reset should show (compiled), got: %s", out)
	}

	// Verify config file still has tool_timeout.
	configPath := filepath.Join(tmpHome, ".agentfile", "test-agent", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config file: %v", err)
	}
	if !strings.Contains(string(data), "120s") {
		t.Errorf("config file should still have tool_timeout, got: %s", data)
	}
}

func TestConfigResetDeletesEmptyFile(t *testing.T) {
	tmpHome := t.TempDir()
	setEnv := func(cmd *exec.Cmd) {
		cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set only model.
	cmd := exec.CommandContext(ctx, binaryPath, "config", "set", "model", "gpt-5")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config set: %v\n%s", err, out)
	}

	// Reset model — file should be deleted.
	cmd = exec.CommandContext(ctx, binaryPath, "config", "reset", "model")
	setEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config reset: %v\n%s", err, out)
	}

	configPath := filepath.Join(tmpHome, ".agentfile", "test-agent", "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		t.Error("config file should be deleted when all fields are reset")
	}
}

func TestServeMCPModelHint(t *testing.T) {
	tmpHome := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Set model override before starting MCP server.
	cmd := exec.CommandContext(ctx, binaryPath, "config", "set", "model", "claude-opus-4-6")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config set: %v\n%s", err, out)
	}

	// Start serve-mcp with the override in effect.
	mcpCmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	mcpCmd.Env = append(os.Environ(), "HOME="+tmpHome)

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "config-integration-test",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: mcpCmd}, nil)
	if err != nil {
		t.Fatalf("connecting to serve-mcp: %v", err)
	}
	defer session.Close()

	// Call get_instructions — should contain model hint.
	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "get_instructions",
	})
	if err != nil {
		t.Fatalf("call get_instructions: %v", err)
	}

	var text string
	for _, c := range result.Content {
		if tc, ok := c.(*gomcp.TextContent); ok {
			text = tc.Text
			break
		}
	}

	if !strings.Contains(text, "claude-opus-4-6") {
		t.Errorf("get_instructions should contain model hint 'claude-opus-4-6', got: %s", text)
	}
	if !strings.Contains(text, "Model Preference") {
		t.Errorf("get_instructions should contain 'Model Preference' header, got: %s", text)
	}
}
