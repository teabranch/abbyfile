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

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

const defaultTimeout = 30 * time.Second

// Executor runs CLI tools as subprocesses.
type Executor struct {
	timeout       time.Duration
	logger        *slog.Logger
	defaultPolicy *CommandPolicy // applied when def.Policy is nil
	hook          ExecutionHook  // optional telemetry callback
	budget        *ContextBudget // nil = no shaping
	spillSink     SpillSink
	sandbox       *sandbox.Sandbox // nil = sandbox.FromContext fallback
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

// WithSandbox sets the sandbox injected into every HandlerCtx call.
func WithSandbox(s *sandbox.Sandbox) ExecutorOption {
	return func(e *Executor) { e.sandbox = s }
}

// WithContextBudget enables output shaping using the given budget and sink.
// A nil sink means spill degrades to head-tail truncation.
func WithContextBudget(b ContextBudget, sink SpillSink) ExecutorOption {
	return func(e *Executor) {
		bc := b
		e.budget = &bc
		e.spillSink = sink
	}
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

// RunRaw executes a tool and returns its output without success-path shaping. Error messages are still shaped.
func (e *Executor) RunRaw(ctx context.Context, def *Definition, input map[string]any) (string, error) {
	if def.Builtin {
		if def.HandlerCtx == nil && def.Handler == nil {
			return "", fmt.Errorf("built-in tool %q has no handler", def.Name)
		}
		e.logger.Info("running builtin tool", "tool", def.Name)
		start := time.Now()
		result, err := e.runBuiltin(ctx, def, input)
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
	ConfigureProcessGroup(cmd)

	if def.StdinInput && input != nil {
		inputJSON, err := json.Marshal(input)
		if err != nil {
			return "", fmt.Errorf("tool %q: marshaling input to JSON: %w", def.Name, err)
		}
		cmd.Stdin = bytes.NewReader(inputJSON)
	}

	limit := e.outputLimit(def)
	stdout, stderr := NewLimitedBuffer(limit), NewLimitedBuffer(limit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()
	// Reap anything the tool left behind in its process group (a background
	// grandchild the direct child didn't wait for) regardless of how Run
	// returned. Cancellation-triggered kills are handled by
	// ConfigureProcessGroup's cmd.Cancel; this covers the normal-exit path,
	// where cmd.Cancel never runs.
	KillProcessGroup(cmd)

	if runErr != nil && errors.Is(runErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() && ctx.Err() == nil {
		// The direct child exited successfully; WaitDelay force-closed the
		// pipes because a grandchild (now reaped above) was still holding
		// them open. The command itself did not fail.
		runErr = nil
	}

	if runErr != nil {
		duration := time.Since(start)
		if e.hook != nil {
			e.hook(def.Name, duration, runErr)
		}
		if ctx.Err() == context.DeadlineExceeded {
			e.logger.Warn("tool timed out", "tool", def.Name, "timeout", e.timeout, "duration", duration)
			return "", fmt.Errorf("tool %q timed out after %s", def.Name, e.timeout)
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			e.logger.Error("CLI tool command not found", "tool", def.Name, "command", def.Command)
			return "", fmt.Errorf("tool %q: command %q not found in PATH", def.Name, def.Command)
		}
		// Include stderr in the error for debugging
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		trimmed := strings.TrimSpace(errMsg)
		shapedErrMsg := e.Shape(def.Name, trimmed)
		e.logger.Error("CLI tool failed", "tool", def.Name, "duration", duration, "error", trimmed)
		return "", fmt.Errorf("tool %q failed: %s", def.Name, shapedErrMsg)
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

// Shape applies the configured budget (if any) to output.
func (e *Executor) Shape(toolName, output string) string {
	if e.budget == nil {
		return output
	}
	return e.budget.Shape(toolName, output, e.spillSink).Output
}

// Run executes a tool and returns budget-shaped output.
func (e *Executor) Run(ctx context.Context, def *Definition, input map[string]any) (string, error) {
	raw, err := e.RunRaw(ctx, def, input)
	if err != nil {
		return "", err
	}
	return e.Shape(def.Name, raw), nil
}

// MaxOutputBytes returns the effective byte cap for toolName, or 0 when no
// budget is configured or the cap is unlimited.
func (e *Executor) MaxOutputBytes(toolName string) int64 {
	if e.budget == nil {
		return 0
	}
	return e.budget.effectiveFor(toolName).MaxOutputBytes
}

// MaxResultSizeCharsCeiling is the largest anthropic/maxResultSizeChars value
// Claude Code accepts.
const MaxResultSizeCharsCeiling = 500000

// ResultSizeHint returns the anthropic/maxResultSizeChars value to advertise
// for toolName. requested reports whether the tool opted in with a per-tool
// InlineLarge. chars is 0 when nothing should be sent: no opt-in, no budget,
// or an unlimited (<=0) effective cap. Caps above the ceiling are clamped.
func (e *Executor) ResultSizeHint(toolName string) (chars int, requested bool) {
	// Called at tool-registration time, so a nil Executor must not panic.
	if e == nil || e.budget == nil {
		return 0, false
	}
	pt, ok := e.budget.PerTool[toolName]
	if !ok || !pt.InlineLarge {
		return 0, false
	}
	limit := e.budget.effectiveFor(toolName).MaxOutputBytes
	if limit <= 0 {
		return 0, true
	}
	return int(min(limit, MaxResultSizeCharsCeiling)), true
}

// runBuiltin calls the tool's handler. HandlerCtx runs under the executor
// timeout (extended to max_command_timeout for UsesCommandTimeout tools)
// with the sandbox and output cap in ctx. Legacy Handler takes no context,
// so no timeout can apply to it.
func (e *Executor) runBuiltin(ctx context.Context, def *Definition, input map[string]any) (string, error) {
	if def.HandlerCtx == nil {
		return def.Handler(input)
	}
	sb := e.sandbox
	if sb == nil {
		sb = sandbox.FromContext(ctx)
	}
	limit := e.timeout
	if def.UsesCommandTimeout {
		limit = max(limit, sb.Config().MaxCommandTimeout)
	}
	hctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	hctx = sandbox.NewContext(hctx, sb)
	hctx = WithOutputLimit(hctx, e.outputLimit(def))
	result, err := def.HandlerCtx(hctx, input)
	if err != nil && errors.Is(hctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("tool %q timed out after %s", def.Name, limit)
	}
	return result, err
}

// outputLimit is the memory cap for captured output: the tool's (or the
// executor's default) CommandPolicy.MaxOutputBytes when positive, else
// DefaultMaxOutputBytes. Zero never means unlimited here.
func (e *Executor) outputLimit(def *Definition) int64 {
	p := def.Policy
	if p == nil {
		p = e.defaultPolicy
	}
	if p != nil && p.MaxOutputBytes > 0 {
		return p.MaxOutputBytes
	}
	return DefaultMaxOutputBytes
}
