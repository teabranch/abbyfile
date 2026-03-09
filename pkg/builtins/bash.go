package builtins

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/teabranch/agentfile/pkg/tools"
)

// RunCommandTool returns a tool definition for shell command execution.
func RunCommandTool() *tools.Definition {
	return tools.BuiltinTool(
		"run_command",
		"Execute an arbitrary shell command via sh -c and return its output. Runs with the permissions of the current user.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds (default: 30)",
				},
			},
			"required": []string{"command"},
		},
		handleRunCommand,
	).WithAnnotations(&tools.Annotations{
		DestructiveHint: tools.BoolPtr(true),
		OpenWorldHint:   tools.BoolPtr(true),
		Title:           "Run Command",
	})
}

// defaultRunCommandPolicy is applied to run_command when no explicit policy is set.
var defaultRunCommandPolicy = tools.DefaultCommandPolicy()

func handleRunCommand(input map[string]any) (string, error) {
	command, ok := input["command"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: command")
	}

	// Enforce command policy.
	if err := defaultRunCommandPolicy.Check(command); err != nil {
		return "", err
	}

	timeout := 30 * time.Second
	if t, ok := input["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("command failed: %w\noutput: %s", err, string(out))
	}
	return string(out), nil
}
