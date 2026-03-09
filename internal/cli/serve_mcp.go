package cli

import (
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/mcp"
	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

// NewServeMCPCommand creates the `serve-mcp` subcommand that starts an
// MCP-over-stdio server exposing all registered tools.
func NewServeMCPCommand(name, version, description string, registry *tools.Registry,
	timeout time.Duration, loader *prompt.Loader, mgr *memory.Manager, logger *slog.Logger, execOpts ...tools.ExecutorOption) *cobra.Command {
	return &cobra.Command{
		Use:   "serve-mcp",
		Short: "Start an MCP server over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			executor := tools.NewExecutor(timeout, logger, execOpts...)
			bridge := mcp.NewBridge(mcp.BridgeConfig{
				Name:        name,
				Version:     version,
				Description: description,
				Registry:    registry,
				Executor:    executor,
				Loader:      loader,
				Memory:      mgr,
				Logger:      logger,
			})
			return bridge.Serve(cmd.Context())
		},
	}
}
