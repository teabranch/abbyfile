package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/tools"
)

// NewRunToolCommand creates the `run-tool` subcommand.
func NewRunToolCommand(registry *tools.Registry, timeout time.Duration, logger *slog.Logger) *cobra.Command {
	var inputJSON string

	cmd := &cobra.Command{
		Use:   "run-tool <name>",
		Short: "Execute a tool by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			def := registry.Get(name)
			if def == nil {
				return fmt.Errorf("unknown tool: %q\nAvailable tools:\n%s", name, formatToolList(registry))
			}

			// Parse input
			var input map[string]any
			if inputJSON != "" {
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return fmt.Errorf("parsing --input JSON: %w", err)
				}
			}

			if err := def.ValidateInput(input); err != nil {
				return fmt.Errorf("invalid input for tool %q: %w", name, err)
			}

			executor := tools.NewExecutor(timeout, logger)
			result, err := executor.Run(context.Background(), def, input)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), result)
			return nil
		},
	}

	cmd.Flags().StringVar(&inputJSON, "input", "", "Tool input as JSON object")

	return cmd
}

func formatToolList(registry *tools.Registry) string {
	var lines []string
	for _, def := range registry.All() {
		desc := def.Description
		if desc == "" {
			desc = "(no description)"
		}
		lines = append(lines, fmt.Sprintf("  - %s: %s", def.Name, desc))
	}
	return strings.Join(lines, "\n")
}
