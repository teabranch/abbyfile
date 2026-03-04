package agent

import (
	"embed"
	"log/slog"
	"time"

	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/tools"
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

// WithLogger sets the structured logger for the agent.
func WithLogger(logger *slog.Logger) Option {
	return func(a *Agent) { a.logger = logger }
}
