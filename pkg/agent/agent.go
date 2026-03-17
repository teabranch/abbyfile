// Package agent is the core runtime for abbyfile binaries. It wires together
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

	"github.com/teabranch/abbyfile/internal/cli"
	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/prompt"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// Agent is the main runtime for an abbyfile binary.
type Agent struct {
	name        string
	version     string
	description string
	model       string

	promptFS   *embed.FS
	promptPath string

	toolDefs    []*tools.Definition
	toolTimeout time.Duration

	memoryEnabled bool
	memoryLimits  memory.Limits

	commandPolicy *tools.CommandPolicy
	executionHook tools.ExecutionHook

	lazyToolLoading bool

	configPath string // override config.yaml path (for testing)

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

	// Load runtime config overrides (after compiled defaults are set).
	var cfg *config.Config
	var cfgErr error
	if a.configPath != "" {
		cfg, cfgErr = config.LoadFrom(a.configPath)
	} else {
		cfg, cfgErr = config.Load(a.name)
	}
	if cfgErr != nil {
		a.logger.Warn("failed to load config override", "error", cfgErr)
	} else if !cfg.IsZero() {
		a.applyConfigOverrides(cfg)
		a.logger.Info("applied config overrides", "path", config.Path(a.name))
	}

	return a, nil
}

// compiledDefaults captures the compiled-in values before config overrides.
func (a *Agent) compiledDefaults() cli.CompiledDefaults {
	return cli.CompiledDefaults{
		Model:       a.model,
		ToolTimeout: a.toolTimeout,
	}
}

// applyConfigOverrides merges non-nil fields from a Config into the agent.
func (a *Agent) applyConfigOverrides(cfg *config.Config) {
	if cfg.Model != nil {
		a.model = *cfg.Model
	}
	if cfg.ToolTimeout != nil {
		if d, err := time.ParseDuration(*cfg.ToolTimeout); err == nil {
			a.toolTimeout = d
		} else {
			a.logger.Warn("invalid tool_timeout in config, keeping default", "value", *cfg.ToolTimeout, "error", err)
		}
	}
	if cfg.MemoryLimits != nil {
		ml := cfg.MemoryLimits
		if ml.MaxKeys != nil {
			a.memoryLimits.MaxKeys = *ml.MaxKeys
		}
		if ml.MaxValueBytes != nil {
			a.memoryLimits.MaxValueBytes = *ml.MaxValueBytes
		}
		if ml.MaxTotalBytes != nil {
			a.memoryLimits.MaxTotalBytes = *ml.MaxTotalBytes
		}
		if ml.TTL != nil {
			if d, err := time.ParseDuration(*ml.TTL); err == nil {
				a.memoryLimits.TTL = d
			} else {
				a.logger.Warn("invalid memory TTL in config, keeping default", "value", *ml.TTL, "error", err)
			}
		}
	}
	if cfg.CommandPolicy != nil {
		cp := cfg.CommandPolicy
		if a.commandPolicy == nil {
			a.commandPolicy = &tools.CommandPolicy{}
		}
		if cp.AllowedPrefixes != nil {
			a.commandPolicy.AllowedPrefixes = *cp.AllowedPrefixes
		}
		if cp.DeniedSubstrings != nil {
			a.commandPolicy.DeniedSubstrings = *cp.DeniedSubstrings
		}
		if cp.MaxOutputBytes != nil {
			a.commandPolicy.MaxOutputBytes = *cp.MaxOutputBytes
		}
	}
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
		Name:          a.name,
		Version:       a.version,
		Description:   a.description,
		Model:         a.model,
		Loader:        loader,
		Registry:      registry,
		Memory:        a.memoryEnabled,
		ToolTimeout:   a.toolTimeout,
		CommandPolicy: a.commandPolicy,
		Logger:        a.logger,
	}
	if a.memoryEnabled {
		cliOpts.MemoryLimits = &a.memoryLimits
	}
	cmd := cli.NewRootCommand(cliOpts)

	// Build executor options from agent config.
	var execOpts []tools.ExecutorOption
	if a.commandPolicy != nil {
		execOpts = append(execOpts, tools.WithDefaultPolicy(a.commandPolicy))
	}
	if a.executionHook != nil {
		execOpts = append(execOpts, tools.WithExecutionHook(a.executionHook))
	}

	// Add subcommands
	cmd.AddCommand(cli.NewRunToolCommand(registry, a.toolTimeout, a.logger, execOpts...))
	cmd.AddCommand(cli.NewServeMCPCommand(a.name, a.version, a.description, a.model, registry, a.toolTimeout, loader, mgr, a.logger, execOpts...))
	cmd.AddCommand(cli.NewValidateCommand(a.name, a.version, loader, registry, a.memoryEnabled))
	cmd.AddCommand(cli.NewConfigCommand(a.name, a.compiledDefaults()))

	if mgr != nil {
		cmd.AddCommand(cli.NewMemoryCommand(mgr))
	}

	if err := cmd.Execute(); err != nil {
		return 1
	}
	return 0
}
