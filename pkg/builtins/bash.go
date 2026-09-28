package builtins

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// RunCommandToolName is the MCP name of the Bash builtin.
const RunCommandToolName = "run_command"

// defaultCommandTimeout applies when the model does not request a timeout.
const defaultCommandTimeout = 30 * time.Second

// RunCommandTool returns the run_command definition. Its description
// assumes the default sandbox; the agent rewrites it at registration with
// RunCommandDescription for the effective sandbox.
func RunCommandTool() *tools.Definition {
	def := tools.BuiltinToolCtx(
		RunCommandToolName,
		RunCommandDescription(sandbox.Default()),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The command to run",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds (default: 30; capped by the agent's sandbox.max_command_timeout)",
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
	def.UsesCommandTimeout = true
	return def
}

// RunCommandDescription describes run_command for the given sandbox mode so
// the model knows what it may run before trying.
func RunCommandDescription(cfg sandbox.Config) string {
	n := cfg.Normalize()
	if n.Bash == sandbox.BashUnrestricted {
		return "Execute a shell command via sh -c and return its combined output. Runs with the permissions of the current user."
	}
	if len(n.AllowCommands) == 0 {
		return "Run an allowlisted command. No commands are allowlisted for this agent, so every call is refused."
	}
	quoted := make([]string, len(n.AllowCommands))
	for i, c := range n.AllowCommands {
		quoted[i] = "`" + c + "`"
	}
	return fmt.Sprintf("Run an allowlisted command without a shell and return its combined output. Allowed: %s (* = any arguments). Pipes, redirects, chaining and substitution are not supported; quote arguments with ' or \".",
		strings.Join(quoted, ", "))
}

// commandTimeout is min(requested seconds or 30s, maxTimeout). t is compared
// against maxTimeout.Seconds() before conversion, so a huge requested value
// saturates at maxTimeout instead of overflowing time.Duration(t*1e9) (a
// float64->int64 conversion whose out-of-range result is platform-dependent).
// A requested value that truncates to zero or less once converted
// (sub-microsecond) floors to 1ms rather than becoming an instantly-expired
// command.
func commandTimeout(input map[string]any, maxTimeout time.Duration) time.Duration {
	timeout := defaultCommandTimeout
	if t, ok := input["timeout"].(float64); ok && t > 0 {
		switch {
		case t >= maxTimeout.Seconds():
			timeout = maxTimeout
		default:
			if d := time.Duration(t * float64(time.Second)); d > 0 {
				timeout = d
			} else {
				timeout = time.Millisecond
			}
		}
	}
	return min(timeout, maxTimeout)
}

func handleRunCommand(ctx context.Context, input map[string]any) (string, error) {
	command, ok := input["command"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: command")
	}
	sb := sandbox.FromContext(ctx)
	cfg := sb.Config()

	var argv []string
	if cfg.Bash == sandbox.BashUnrestricted {
		argv = []string{"sh", "-c", command}
	} else {
		var err error
		if argv, err = sb.CheckCommand(command); err != nil {
			return "", err
		}
	}

	timeout := commandTimeout(input, cfg.MaxCommandTimeout)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = sb.Cwd()
	tools.ConfigureProcessGroup(cmd)
	out := tools.NewLimitedBuffer(tools.OutputLimit(ctx))
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	// Reap anything the command left behind in its process group (a
	// background grandchild the direct child didn't wait for) regardless of
	// how Run returned. Cancellation-triggered kills are handled by
	// ConfigureProcessGroup's cmd.Cancel; this covers the normal-exit path,
	// where cmd.Cancel never runs.
	tools.KillProcessGroup(cmd)
	if err != nil && errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() && cctx.Err() == nil {
		// The direct child exited successfully; WaitDelay force-closed the
		// pipes because a grandchild (now reaped above) was still holding
		// them open. The command itself did not fail.
		err = nil
	}
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("command timed out after %s (sandbox.max_command_timeout is %s)\noutput: %s", timeout, cfg.MaxCommandTimeout, out.String())
	}
	if err != nil {
		return "", fmt.Errorf("command failed: %w\noutput: %s", err, out.String())
	}
	return out.String(), nil
}
