package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/registry"
)

func newUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <agent-name>",
		Short: "Remove an installed agent",
		Long: `Removes an agent binary, unwires it from MCP config, and removes it
from the registry. Uses the registry to find the binary path and scope.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(args[0])
		},
	}
}

func runUninstall(name string) error {
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

	// Unwire from MCP config.
	mcpPath := ".mcp.json"
	if entry.Scope == "global" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home dir: %w", err)
		}
		mcpPath = filepath.Join(home, ".claude", "mcp.json")
	}
	if err := removeMCPEntry(mcpPath, name); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update %s: %v\n", mcpPath, err)
	} else {
		fmt.Printf("Updated %s\n", mcpPath)
	}

	// Remove from registry.
	reg.Remove(name)
	if err := reg.Save(); err != nil {
		return fmt.Errorf("saving registry: %w", err)
	}
	fmt.Printf("Uninstalled %s\n", name)
	return nil
}

// removeMCPEntry removes a single server entry from an MCP config file.
func removeMCPEntry(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}

	delete(servers, name)
	cfg["mcpServers"] = servers

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}
