package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Executor runs CLI tools as subprocesses.
type Executor struct {
	timeout       time.Duration
	logger        *slog.Logger
	defaultPolicy *CommandPolicy // applied when def.Policy is nil
	hook          ExecutionHook  // optional telemetry callback
}

// ExecutionHook is called after each tool execution with timing and error info.
type ExecutionHook func(tool string, duration time.Duration, err error)

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

// WithDefaultPolicy sets a default CommandPolicy applied when a tool has no policy.
func WithDefaultPolicy(p *CommandPolicy) ExecutorOption {
	return func(e *Executor) { e.defaultPolicy = p }
}

// WithExecutionHook sets a callback invoked after each tool execution.
func WithExecutionHook(h ExecutionHook) ExecutorOption {
	return func(e *Executor) { e.hook = h }
}

// NewExecutor creates a new Executor with the given timeout and logger.
// A nil logger disables logging.
func NewExecutor(timeout time.Duration, logger *slog.Logger, opts ...ExecutorOption) *Executor {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &Executor{timeout: timeout, logger: logger}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run executes a tool definition with the given input and returns the output.
func (e *Executor) Run(ctx context.Context, def *Definition, input map[string]any) (string, error) {
	if def.Builtin {
		if def.Handler == nil {
			return "", fmt.Errorf("built-in tool %q has no handler", def.Name)
		}
		e.logger.Info("running builtin tool", "tool", def.Name)
		start := time.Now()
		result, err := def.Handler(input)
		duration := time.Since(start)
		if e.hook != nil {
			e.hook(def.Name, duration, err)
		}
		if err != nil {
			e.logger.Error("builtin tool failed", "tool", def.Name, "duration", duration, "error", err)
			return "", err
		}
		e.logger.Info("builtin tool completed", "tool", def.Name, "duration", duration)
		return result, nil
	}

	if def.Command == "" {
		return "", fmt.Errorf("tool %q has no command", def.Name)
	}

	// Build argument list
	args := make([]string, len(def.Args))
	copy(args, def.Args)

	// When StdinInput is true, pipe the full input as JSON to stdin
	// instead of appending args from input.
	var commandStr string
	if !def.StdinInput {
		// Append any args from input
		if argsStr, ok := input["args"].(string); ok && argsStr != "" {
			commandStr = argsStr
			parts := strings.Fields(argsStr)
			args = append(args, parts...)
		}
	}

	// Check command policy.
	policy := def.Policy
	if policy == nil {
		policy = e.defaultPolicy
	}
	if policy != nil && commandStr != "" {
		if err := policy.Check(commandStr); err != nil {
			return "", fmt.Errorf("tool %q: %w", def.Name, err)
		}
	}

	e.logger.Info("running CLI tool", "tool", def.Name, "command", def.Command, "args", args)
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, def.Command, args...)

	if def.StdinInput && input != nil {
		inputJSON, err := json.Marshal(input)
		if err != nil {
			return "", fmt.Errorf("tool %q: marshaling input to JSON: %w", def.Name, err)
		}
		cmd.Stdin = bytes.NewReader(inputJSON)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		duration := time.Since(start)
		if e.hook != nil {
			e.hook(def.Name, duration, err)
		}
		if ctx.Err() == context.DeadlineExceeded {
			e.logger.Warn("tool timed out", "tool", def.Name, "timeout", e.timeout, "duration", duration)
			return "", fmt.Errorf("tool %q timed out after %s", def.Name, e.timeout)
		}
		if errors.Is(err, exec.ErrNotFound) {
			e.logger.Error("CLI tool command not found", "tool", def.Name, "command", def.Command)
			return "", fmt.Errorf("tool %q: command %q not found in PATH", def.Name, def.Command)
		}
		// Include stderr in the error for debugging
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		e.logger.Error("CLI tool failed", "tool", def.Name, "duration", duration, "error", strings.TrimSpace(errMsg))
		return "", fmt.Errorf("tool %q failed: %s", def.Name, strings.TrimSpace(errMsg))
	}

	duration := time.Since(start)
	if e.hook != nil {
		e.hook(def.Name, duration, nil)
	}
	e.logger.Info("CLI tool completed", "tool", def.Name, "duration", duration)

	result := stdout.String()
	if result == "" {
		result = stderr.String()
	}
	return strings.TrimSpace(result), nil
}
