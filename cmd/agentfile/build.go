package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/builder"
	"github.com/teabranch/agentfile/pkg/definition"
)

func newBuildCommand() *cobra.Command {
	var (
		agentfilePath string
		outputDir     string
		agentName     string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build agent binaries from an Agentfile",
		Long: `Parses the Agentfile, reads each agent's .md file, generates Go source,
and compiles standalone binaries into the output directory.

Also generates/updates .mcp.json with serve-mcp entries for each agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(agentfilePath, outputDir, agentName)
		},
	}

	cmd.Flags().StringVarP(&agentfilePath, "file", "f", "Agentfile", "Path to Agentfile")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "./build", "Output directory for binaries")
	cmd.Flags().StringVar(&agentName, "agent", "", "Build a single agent by name")

	return cmd
}

func runBuild(agentfilePath, outputDir, agentName string) error {
	af, err := definition.ParseAgentfile(agentfilePath)
	if err != nil {
		return err
	}

	// Resolve base dir for relative .md paths.
	baseDir := filepath.Dir(agentfilePath)
	if !filepath.IsAbs(baseDir) {
		cwd, _ := os.Getwd()
		baseDir = filepath.Join(cwd, baseDir)
	}

	// Auto-detect replace directive for local development.
	moduleDir := builder.DetectModuleDir()

	cfg := builder.BuildConfig{
		OutputDir: outputDir,
		ModuleDir: moduleDir,
	}

	defs := make(map[string]*definition.AgentDef)

	for name, ref := range af.Agents {
		if agentName != "" && name != agentName {
			continue
		}

		mdPath := ref.Path
		if !filepath.IsAbs(mdPath) {
			mdPath = filepath.Join(baseDir, mdPath)
		}

		def, err := definition.ParseAgentMD(mdPath)
		if err != nil {
			return fmt.Errorf("parsing agent %q: %w", name, err)
		}
		// Use the Agentfile key as the binary name, version from Agentfile.
		def.Name = name
		def.Version = ref.Version
		defs[name] = def
	}

	if agentName != "" && len(defs) == 0 {
		return fmt.Errorf("agent %q not found in Agentfile", agentName)
	}

	if err := builder.BuildAll(defs, cfg); err != nil {
		return err
	}

	// Generate .mcp.json with serve-mcp entries.
	absOut, _ := filepath.Abs(outputDir)
	entries := make(map[string]MCPServerEntry)
	for name := range defs {
		entries[name] = MCPServerEntry{
			Command: filepath.Join(absOut, name),
			Args:    []string{"serve-mcp"},
		}
	}

	if err := mergeMCPJSON(".mcp.json", entries); err != nil {
		return fmt.Errorf("updating .mcp.json: %w", err)
	}
	fmt.Println("Updated .mcp.json")

	return nil
}
