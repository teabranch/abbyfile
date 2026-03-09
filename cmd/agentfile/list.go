package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/registry"
)

func newListCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed agents",
		Long:  `Shows all agents tracked in the registry with their version, source, and scope.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func runList(jsonOutput bool) error {
	regPath, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	entries := reg.List()
	if len(entries) == 0 {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Println("No agents installed.")
		}
		return nil
	}

	// Sort by name for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	if jsonOutput {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling entries: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tSOURCE\tSCOPE\tINSTALLED\tPATH")
	for _, e := range entries {
		installed := e.InstalledAt
		if len(installed) > 10 {
			installed = installed[:10] // show date only
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Name, e.Version, e.Source, e.Scope, installed, e.Path)
	}
	return w.Flush()
}
