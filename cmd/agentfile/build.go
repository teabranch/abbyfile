package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/builder"
	"github.com/teabranch/agentfile/pkg/definition"
	"github.com/teabranch/agentfile/pkg/plugin"
)

// loadSkillFiles reads skill file contents from disk, resolving paths
// relative to the agent .md file's directory.
func loadSkillFiles(def *definition.AgentDef, agentMDDir string) ([]plugin.SkillFile, error) {
	var skills []plugin.SkillFile
	for _, s := range def.Skills {
		path := s.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(agentMDDir, path)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading skill %q at %s: %w", s.Name, path, err)
		}

		skills = append(skills, plugin.SkillFile{
			Name:        s.Name,
			Description: s.Description,
			Content:     string(data),
		})
	}
	return skills, nil
}

func newBuildCommand() *cobra.Command {
	var (
		agentfilePath string
		outputDir     string
		agentName     string
		pluginFlag    bool
		parallelism   int
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build agent binaries from an Agentfile",
		Long: `Parses the Agentfile, reads each agent's .md file, generates Go source,
and compiles standalone binaries into the output directory.

Also generates/updates .mcp.json with serve-mcp entries for each agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(agentfilePath, outputDir, agentName, pluginFlag, parallelism)
		},
	}

	cmd.Flags().StringVarP(&agentfilePath, "file", "f", "", "Path to Agentfile")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "./build", "Output directory for binaries")
	cmd.Flags().StringVar(&agentName, "agent", "", "Build a single agent by name")
	cmd.Flags().BoolVar(&pluginFlag, "plugin", false, "Also generate a Claude Code plugin directory")
	cmd.Flags().IntVar(&parallelism, "parallelism", 0, "Max concurrent agent builds (0 = sequential)")

	return cmd
}

func runBuild(agentfilePath, outputDir, agentName string, pluginOutput bool, parallelism int) error {
	if agentfilePath == "" {
		agentfilePath = resolveAgentfile()
	}

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
		OutputDir:   outputDir,
		ModuleDir:   moduleDir,
		Parallelism: parallelism,
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

	// Generate plugin directories if --plugin flag is set.
	if pluginOutput {
		for name, def := range defs {
			mdPath := af.Agents[name].Path
			if !filepath.IsAbs(mdPath) {
				mdPath = filepath.Join(baseDir, mdPath)
			}
			agentMDDir := filepath.Dir(mdPath)

			skills, err := loadSkillFiles(def, agentMDDir)
			if err != nil {
				return fmt.Errorf("loading skills for %s: %w", name, err)
			}

			binaryPath := filepath.Join(absOut, name)
			if err := plugin.Generate(def, skills, plugin.GenerateConfig{
				OutputDir:  outputDir,
				BinaryPath: binaryPath,
			}); err != nil {
				return fmt.Errorf("generating plugin for %s: %w", name, err)
			}
			fmt.Fprintf(os.Stderr, "→ %s/%s.claude-plugin/\n", outputDir, name)
		}
	}

	return nil
}
