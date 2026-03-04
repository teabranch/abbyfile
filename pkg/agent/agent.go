// Package agent is the core runtime for agentfile binaries. It wires together
// prompt loading, tool registration, memory management, and the CLI interface.
//
// The binary does NOT call the Claude API. Claude Code is the LLM runtime —
// the binary is a packaging format that exposes everything the agent needs
// through CLI subcommands.
package agent

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/teabranch/agentfile/internal/cli"
	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

// Agent is the main runtime for an agentfile binary.
type Agent struct {
	name        string
	version     string
	description string

	promptFS   *embed.FS
	promptPath string

	toolDefs    []*tools.Definition
	toolTimeout time.Duration

	memoryEnabled bool
	memoryLimits  memory.Limits

	logger *slog.Logger
}

// New creates a new Agent with the given options.
func New(opts ...Option) (*Agent, error) {
	a := &Agent{
		toolTimeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(a)
	}

	if a.name == "" {
		return nil, fmt.Errorf("agent name is required (use WithName)")
	}
	if a.version == "" {
		return nil, fmt.Errorf("agent version is required (use WithVersion)")
	}
	if a.promptFS == nil {
		return nil, fmt.Errorf("prompt filesystem is required (use WithPromptFS)")
	}

	if a.logger == nil {
		a.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	return a, nil
}

// Execute sets up the CLI and runs the agent binary. Returns an exit code.
func (a *Agent) Execute() int {
	loader := prompt.NewLoader(a.name, *a.promptFS, a.promptPath)

	// Register all tools
	registry := tools.NewRegistry()
	for _, def := range a.toolDefs {
		if err := registry.Register(def); err != nil {
			fmt.Fprintf(os.Stderr, "Error registering tool %q: %v\n", def.Name, err)
			return 1
		}
	}

	// Set up memory if enabled
	var mgr *memory.Manager
	if a.memoryEnabled {
		store, err := memory.NewFileStore(a.name, a.memoryLimits)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing memory: %v\n", err)
			return 1
		}
		mgr = memory.NewManager(store)

		// Register memory tools so they appear in --describe
		for _, def := range mgr.Tools() {
			if err := registry.Register(def); err != nil {
				fmt.Fprintf(os.Stderr, "Error registering memory tool %q: %v\n", def.Name, err)
				return 1
			}
		}
	}

	a.logger.Info("agent starting", "name", a.name, "version", a.version, "tools", len(registry.All()), "memory", a.memoryEnabled)

	// Build root command
	cliOpts := cli.Options{
		Name:        a.name,
		Version:     a.version,
		Description: a.description,
		Loader:      loader,
		Registry:    registry,
		Memory:      a.memoryEnabled,
		Logger:      a.logger,
	}
	if a.memoryEnabled {
		cliOpts.MemoryLimits = &a.memoryLimits
	}
	cmd := cli.NewRootCommand(cliOpts)

	// Add subcommands
	cmd.AddCommand(cli.NewRunToolCommand(registry, a.toolTimeout, a.logger))
	cmd.AddCommand(cli.NewServeMCPCommand(a.name, a.version, a.description, registry, a.toolTimeout, loader, mgr, a.logger))
	cmd.AddCommand(cli.NewValidateCommand(a.name, a.version, loader, registry, a.memoryEnabled))

	if mgr != nil {
		cmd.AddCommand(cli.NewMemoryCommand(mgr))
	}

	if err := cmd.Execute(); err != nil {
		return 1
	}
	return 0
}
