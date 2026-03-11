// Package main is the agentfile CLI — builds declarative agent definitions
// into standalone CLI binaries that integrate with Claude Code via MCP.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const cliVersion = "0.5.0"

func main() {
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentfile",
		Short: "Build and manage Agentfile agents",
		Long: `agentfile builds declarative agent definitions into standalone CLI binaries
that integrate with Claude Code via the MCP protocol.

Declare agents in an Agentfile (YAML) pointing to .md files with prompts
and tool declarations, then run 'agentfile build' to get binaries.`,
	}

	cmd.Version = cliVersion
	cmd.SetVersionTemplate("agentfile v{{.Version}}\n")

	cmd.AddCommand(newBuildCommand())
	cmd.AddCommand(newInstallCommand())
	cmd.AddCommand(newPublishCommand())
	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newUninstallCommand())
	cmd.AddCommand(newUpdateCommand())
	cmd.AddCommand(newDiffCommand())

	return cmd
}
