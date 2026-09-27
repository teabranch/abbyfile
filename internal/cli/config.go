package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// CompiledDefaults captures the compiled-in values before config overrides are applied.
// Used by the config subcommand to show what the binary was built with.
type CompiledDefaults struct {
	Model             string
	ToolTimeout       time.Duration
	MaxOutputLines    int
	MaxOutputBytes    int64
	OnOverflow        string
	EagerInstructions bool
	Sandbox           sandbox.Config
}

// NewConfigCommand creates the `config` subcommand for inspecting and
// modifying runtime config overrides.
func NewConfigCommand(name string, defaults CompiledDefaults) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and modify runtime configuration overrides",
	}

	cmd.AddCommand(newConfigGetCommand(name, defaults))
	cmd.AddCommand(newConfigSetCommand(name))
	cmd.AddCommand(newConfigResetCommand(name))
	cmd.AddCommand(newConfigPathCommand(name))

	return cmd
}

func newConfigGetCommand(name string, defaults CompiledDefaults) *cobra.Command {
	return &cobra.Command{
		Use:   "get [field]",
		Short: "Show configuration (compiled defaults + overrides)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(name)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if len(args) == 1 {
				return printField(cmd, args[0], cfg, defaults)
			}

			// Show all fields with effective values.
			printAllFields(cmd, cfg, defaults)
			return nil
		},
	}
}

func newConfigSetCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <field> <value>",
		Short: "Set a config override",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			field, value := args[0], args[1]
			if err := config.WriteField(name, field, value); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %s (override saved)\n", field, value)
			if strings.HasPrefix(field, "sandbox.") {
				if w := sandboxSetWarning(field, value); w != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+w)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Restart the runtime session (e.g. Claude Code) so running agents pick this up.")
			}
			return nil
		},
	}
}

func newConfigResetCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use:   "reset <field>",
		Short: "Remove a config override, reverting to compiled default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			field := args[0]
			if err := config.ResetField(name, field); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s reset to compiled default\n", field)
			return nil
		},
	}
}

func newConfigPathCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := config.Path(name)
			if p == "" {
				return fmt.Errorf("cannot resolve home directory")
			}
			fmt.Fprintln(cmd.OutOrStdout(), p)
			return nil
		},
	}
}

type sandboxField struct{ key, value, source string }

// sandboxFields returns the effective sandbox.* values and their source.
func sandboxFields(cfg *config.Config, defaults CompiledDefaults) []sandboxField {
	d := defaults.Sandbox.Normalize()
	var so config.SandboxOverride
	if cfg.Sandbox != nil {
		so = *cfg.Sandbox
	}
	src := func(set bool) string {
		if set {
			return "override"
		}
		return "compiled"
	}
	list := func(v []string) string {
		b, _ := json.Marshal(append([]string{}, v...))
		return string(b)
	}
	dirs, bash, cmds, timeout := d.AllowedDirs, string(d.Bash), d.AllowCommands, d.MaxCommandTimeout.String()
	if so.AllowedDirs != nil {
		dirs = *so.AllowedDirs
	}
	if so.Bash != nil {
		bash = *so.Bash
	}
	if so.AllowCommands != nil {
		cmds = *so.AllowCommands
	}
	if so.MaxCommandTimeout != nil {
		timeout = *so.MaxCommandTimeout
	}
	return []sandboxField{
		{"sandbox.allowed_dirs", list(dirs), src(so.AllowedDirs != nil)},
		{"sandbox.bash", bash, src(so.Bash != nil)},
		{"sandbox.allow_commands", list(cmds), src(so.AllowCommands != nil)},
		{"sandbox.max_command_timeout", timeout, src(so.MaxCommandTimeout != nil)},
	}
}

// sandboxSetWarning returns a warning for risky sandbox values, or "".
func sandboxSetWarning(field, value string) string {
	switch field {
	case "sandbox.bash":
		if value == string(sandbox.BashUnrestricted) {
			return "run_command will run any shell command with your user's permissions"
		}
	case "sandbox.allowed_dirs":
		if dirs, err := config.ParseList(value); err == nil && slices.Contains(dirs, "/") {
			return "allowed_dirs includes / — file tools can reach the whole filesystem"
		}
	}
	return ""
}

// printField prints a single field's effective value.
func printField(cmd *cobra.Command, field string, cfg *config.Config, defaults CompiledDefaults) error {
	switch field {
	case "model":
		val := defaults.Model
		source := "compiled"
		if cfg.Model != nil {
			val = *cfg.Model
			source = "override"
		}
		if val == "" {
			val = "(not set)"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", val, source)
	case "tool_timeout":
		val := defaults.ToolTimeout.String()
		source := "compiled"
		if cfg.ToolTimeout != nil {
			val = *cfg.ToolTimeout
			source = "override"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", val, source)
	case "context_budget.max_output_lines":
		val := defaults.MaxOutputLines
		source := "compiled"
		if cfg.ContextBudget != nil && cfg.ContextBudget.MaxOutputLines != nil {
			val = *cfg.ContextBudget.MaxOutputLines
			source = "override"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d (%s)\n", val, source)
	case "context_budget.max_output_bytes":
		val := defaults.MaxOutputBytes
		source := "compiled"
		if cfg.ContextBudget != nil && cfg.ContextBudget.MaxOutputBytes != nil {
			val = *cfg.ContextBudget.MaxOutputBytes
			source = "override"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d (%s)\n", val, source)
	case "context_budget.on_overflow":
		val := defaults.OnOverflow
		source := "compiled"
		if cfg.ContextBudget != nil && cfg.ContextBudget.OnOverflow != nil {
			val = *cfg.ContextBudget.OnOverflow
			source = "override"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", val, source)
	case "context_budget.eager_instructions":
		val := defaults.EagerInstructions
		source := "compiled"
		if cfg.ContextBudget != nil && cfg.ContextBudget.EagerInstructions != nil {
			val = *cfg.ContextBudget.EagerInstructions
			source = "override"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%t (%s)\n", val, source)
	case "sandbox.allowed_dirs", "sandbox.bash", "sandbox.allow_commands", "sandbox.max_command_timeout":
		for _, f := range sandboxFields(cfg, defaults) {
			if f.key == field {
				fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", f.value, f.source)
			}
		}
	default:
		return fmt.Errorf("unknown field: %s (supported: model, tool_timeout, context_budget.max_output_lines, context_budget.max_output_bytes, context_budget.on_overflow, context_budget.eager_instructions, sandbox.allowed_dirs, sandbox.bash, sandbox.allow_commands, sandbox.max_command_timeout)", field)
	}
	return nil
}

// printAllFields prints all fields with their effective values and source.
func printAllFields(cmd *cobra.Command, cfg *config.Config, defaults CompiledDefaults) {
	w := cmd.OutOrStdout()

	// model
	modelVal := defaults.Model
	modelSource := "compiled"
	if cfg.Model != nil {
		modelVal = *cfg.Model
		modelSource = "override"
	}
	if modelVal == "" {
		modelVal = "(not set)"
	}
	fmt.Fprintf(w, "model: %s (%s)\n", modelVal, modelSource)

	// tool_timeout
	timeoutVal := defaults.ToolTimeout.String()
	timeoutSource := "compiled"
	if cfg.ToolTimeout != nil {
		timeoutVal = *cfg.ToolTimeout
		timeoutSource = "override"
	}
	if defaults.ToolTimeout == 0 {
		timeoutVal = time.Duration(30 * time.Second).String()
	}
	fmt.Fprintf(w, "tool_timeout: %s (%s)\n", timeoutVal, timeoutSource)

	// context_budget.max_output_lines
	maxLinesVal := defaults.MaxOutputLines
	maxLinesSource := "compiled"
	if cfg.ContextBudget != nil && cfg.ContextBudget.MaxOutputLines != nil {
		maxLinesVal = *cfg.ContextBudget.MaxOutputLines
		maxLinesSource = "override"
	}
	fmt.Fprintf(w, "context_budget.max_output_lines: %d (%s)\n", maxLinesVal, maxLinesSource)

	// context_budget.max_output_bytes
	maxBytesVal := defaults.MaxOutputBytes
	maxBytesSource := "compiled"
	if cfg.ContextBudget != nil && cfg.ContextBudget.MaxOutputBytes != nil {
		maxBytesVal = *cfg.ContextBudget.MaxOutputBytes
		maxBytesSource = "override"
	}
	fmt.Fprintf(w, "context_budget.max_output_bytes: %d (%s)\n", maxBytesVal, maxBytesSource)

	// context_budget.on_overflow
	onOverflowVal := defaults.OnOverflow
	onOverflowSource := "compiled"
	if cfg.ContextBudget != nil && cfg.ContextBudget.OnOverflow != nil {
		onOverflowVal = *cfg.ContextBudget.OnOverflow
		onOverflowSource = "override"
	}
	fmt.Fprintf(w, "context_budget.on_overflow: %s (%s)\n", onOverflowVal, onOverflowSource)

	// context_budget.eager_instructions
	eagerVal := defaults.EagerInstructions
	eagerSource := "compiled"
	if cfg.ContextBudget != nil && cfg.ContextBudget.EagerInstructions != nil {
		eagerVal = *cfg.ContextBudget.EagerInstructions
		eagerSource = "override"
	}
	fmt.Fprintf(w, "context_budget.eager_instructions: %t (%s)\n", eagerVal, eagerSource)

	for _, f := range sandboxFields(cfg, defaults) {
		fmt.Fprintf(w, "%s: %s (%s)\n", f.key, f.value, f.source)
	}

	// memory_limits
	if cfg.MemoryLimits != nil {
		fmt.Fprintf(w, "memory_limits: (override)\n")
	}

	// command_policy
	if cfg.CommandPolicy != nil {
		fmt.Fprintf(w, "command_policy: (override)\n")
	}
}
