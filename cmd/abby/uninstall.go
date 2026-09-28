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
	var dryRun bool
	var configMethod string

	cmd := &cobra.Command{
		Use:   "uninstall <agent-name>",
		Short: "Remove an installed agent",
		Long: `Removes an agent binary, unwires it from MCP config for all detected
runtimes, and removes it from the registry. Use --runtime to target a
specific runtime or "all" for all supported runtimes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgOpts, err := configOptions(configMethod)
			if err != nil {
				return err
			}
			return runUninstall(args[0], runtimeFlag, cfgOpts, dryRun)
		},
	}

	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Target runtime: auto, all, claude-code, codex, gemini")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show planned changes without removing anything")
	cmd.Flags().StringVar(&configMethod, "config-method", "", "auto (default; env ABBY_CONFIG_METHOD), cli, or file")

	return cmd
}

func runUninstall(name, runtimeFlag string, cfgOpts runtimecfg.Options, dryRun bool) error {
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

	// uninstall may run from anywhere; resolve config paths against the
	// entry's own project root, not the process's cwd.
	cfgOpts.ProjectRoot = projectRootFor(entry)
	writers, err := runtimecfg.Resolve(runtimeFlag, cfgOpts)
	if err != nil {
		return err
	}

	opts := installOptions{
		Global:      entry.Scope == "global",
		DryRun:      dryRun,
		ProjectRoot: cfgOpts.ProjectRoot,
		Writers:     writers,
		Out:         os.Stdout,
		Err:         os.Stderr,
	}

	if dryRun {
		fmt.Fprintf(opts.Out, "would remove %s\n", entry.Path)
	} else {
		// Remove binary.
		if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing binary: %w", err)
		}
		fmt.Fprintf(opts.Out, "Removed %s\n", entry.Path)
	}

	// Unwire from MCP config for all target runtimes.
	scope := scopeFor(opts.Global)
	applied, err := removeEntries(opts, scope, name)
	printSummary(opts.Out, applied, opts.DryRun)
	if err != nil {
		return err
	}

	if dryRun {
		return nil
	}

	// Remove from registry.
	reg.Remove(name)
	if err := reg.Save(); err != nil {
		return fmt.Errorf("saving registry: %w", err)
	}
	fmt.Fprintf(opts.Out, "Uninstalled %s\n", name)
	return nil
}
