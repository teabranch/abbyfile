package agent

import (
	"embed"
	"log/slog"
	"time"

	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// Option configures an Agent.
type Option func(*Agent)

// WithName sets the agent name.
func WithName(name string) Option {
	return func(a *Agent) { a.name = name }
}

// WithVersion sets the agent version.
func WithVersion(version string) Option {
	return func(a *Agent) { a.version = version }
}

// WithDescription sets the agent description.
func WithDescription(desc string) Option {
	return func(a *Agent) { a.description = desc }
}

// WithModel sets the agent's model hint (informational metadata).
func WithModel(model string) Option {
	return func(a *Agent) { a.model = model }
}

// WithPromptFS sets the embedded filesystem and path for the system prompt.
func WithPromptFS(fs embed.FS, path string) Option {
	return func(a *Agent) {
		a.promptFS = &fs
		a.promptPath = path
	}
}

// WithTools registers CLI tools for the agent.
func WithTools(defs ...*tools.Definition) Option {
	return func(a *Agent) {
		a.toolDefs = append(a.toolDefs, defs...)
	}
}

// WithToolTimeout sets the timeout for tool execution.
func WithToolTimeout(d time.Duration) Option {
	return func(a *Agent) { a.toolTimeout = d }
}

// WithMemory enables per-agent persistent memory.
func WithMemory(enabled bool) Option {
	return func(a *Agent) { a.memoryEnabled = enabled }
}

// WithMemoryLimits sets capacity limits for the agent's memory store.
func WithMemoryLimits(limits memory.Limits) Option {
	return func(a *Agent) { a.memoryLimits = limits }
}

// WithCommandPolicy sets a default command policy for tool execution.
func WithCommandPolicy(p *tools.CommandPolicy) Option {
	return func(a *Agent) { a.commandPolicy = p }
}

// WithExecutionHook sets a callback invoked after each tool execution.
func WithExecutionHook(h tools.ExecutionHook) Option {
	return func(a *Agent) { a.executionHook = h }
}

// WithLogger sets the structured logger for the agent.
func WithLogger(logger *slog.Logger) Option {
	return func(a *Agent) { a.logger = logger }
}

// WithLazyToolLoading is kept for source compatibility only.
//
// Deprecated: lazy tool loading was removed in v0.10.0; every tool is always
// registered. Passing true logs a warning. It will be removed in a future
// release.
func WithLazyToolLoading(enabled bool) Option {
	return func(a *Agent) { a.lazyToolLoading = enabled }
}

// WithConfigPath overrides the default config.yaml location.
// Primarily useful for testing.
func WithConfigPath(path string) Option {
	return func(a *Agent) { a.configPath = path }
}

// WithContextBudget sets the compiled-in context budget for the agent.
func WithContextBudget(b tools.ContextBudget) Option {
	return func(a *Agent) { a.budget = b }
}
