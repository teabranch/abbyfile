package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func newUninstallCommand() *cobra.Command {
	var runtimeFlag string

	cmd := &cobra.Command{
		Use:   "uninstall <agent-name>",
		Short: "Remove an installed agent",
		Long: `Removes an agent binary, unwires it from MCP config for all detected
runtimes, and removes it from the registry. Use --runtime to target a
specific runtime or "all" for all supported runtimes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			writers, err := runtimecfg.Resolve(runtimeFlag)
			if err != nil {
				return err
			}
			return runUninstall(args[0], writers)
		},
	}

	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Target runtime: auto, all, claude-code, codex, gemini")

	return cmd
}

func runUninstall(name string, writers []runtimecfg.ConfigWriter) error {
	regPath, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	entry, ok := reg.Get(name)
	if !ok {
		return fmt.Errorf("agent %q is not installed (not found in registry)", name)
	}

	// Remove binary.
	if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing binary: %w", err)
	}
	fmt.Printf("Removed %s\n", entry.Path)

	// Unwire from MCP config for all target runtimes.
	global := entry.Scope == "global"
	for _, w := range writers {
		var cfgPath string
		if global {
			cfgPath, err = w.GlobalPath()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not resolve global path for %s: %v\n", w.Runtime(), err)
				continue
			}
		} else {
			cfgPath = w.LocalPath()
		}
		if err := w.Remove(cfgPath, name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not update %s (%s): %v\n", cfgPath, w.Runtime(), err)
		} else {
			fmt.Printf("Updated %s (%s)\n", cfgPath, w.Runtime())
		}
	}

	// Remove from registry.
	reg.Remove(name)
	if err := reg.Save(); err != nil {
		return fmt.Errorf("saving registry: %w", err)
	}
	fmt.Printf("Uninstalled %s\n", name)
	return nil
}
