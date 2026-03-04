package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/memory"
)

// NewMemoryCommand creates the `memory` subcommand group.
func NewMemoryCommand(mgr *memory.Manager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage the agent's persistent memory",
	}

	cmd.AddCommand(
		newMemoryReadCmd(mgr),
		newMemoryWriteCmd(mgr),
		newMemoryListCmd(mgr),
		newMemoryDeleteCmd(mgr),
		newMemoryAppendCmd(mgr),
	)

	return cmd
}

func newMemoryReadCmd(mgr *memory.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "read <key>",
		Short: "Read a value from memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			val, err := mgr.Get(args[0])
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					return fmt.Errorf("%w\nHint: run '%s memory list' to see available keys", err, cmd.Root().Name())
				}
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), val)
			return nil
		},
	}
}

func newMemoryWriteCmd(mgr *memory.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "write <key> <value>",
		Short: "Write a value to memory (overwrites existing)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.Set(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stored %d bytes under key %q\n", len(args[1]), args[0])
			return nil
		},
	}
}

func newMemoryListCmd(mgr *memory.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all memory keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			keys, err := mgr.Keys()
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No keys in memory.")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), strings.Join(keys, "\n"))
			return nil
		},
	}
}

func newMemoryDeleteCmd(mgr *memory.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a key from memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.Delete(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted key %q\n", args[0])
			return nil
		},
	}
}

func newMemoryAppendCmd(mgr *memory.Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "append <key> <value>",
		Short: "Append content to an existing memory key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.Append(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Appended %d bytes to key %q\n", len(args[1]), args[0])
			return nil
		},
	}
}
