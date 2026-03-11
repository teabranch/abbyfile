package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/config"
)

// CompiledDefaults captures the compiled-in values before config overrides are applied.
// Used by the config subcommand to show what the binary was built with.
type CompiledDefaults struct {
	Model       string
	ToolTimeout time.Duration
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
	default:
		return fmt.Errorf("unknown field: %s (supported: model, tool_timeout)", field)
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

	// memory_limits
	if cfg.MemoryLimits != nil {
		fmt.Fprintf(w, "memory_limits: (override)\n")
	}

	// command_policy
	if cfg.CommandPolicy != nil {
		fmt.Fprintf(w, "command_policy: (override)\n")
	}
}
