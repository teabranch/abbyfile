package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/builder"
	"github.com/teabranch/abbyfile/pkg/definition"
	"github.com/teabranch/abbyfile/pkg/plugin"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
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
		abbyfilePath string
		outputDir    string
		agentName    string
		pluginFlag   bool
		parallelism  int
		runtimeFlag  string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build agent binaries from an Abbyfile",
		Long: `Parses the Abbyfile, reads each agent's .md file, generates Go source,
and compiles standalone binaries into the output directory.

Also generates/updates MCP config for detected runtimes (Claude Code, Codex, Gemini).
Use --runtime to target a specific runtime or "all" for all supported runtimes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(abbyfilePath, outputDir, agentName, pluginFlag, parallelism, runtimeFlag)
		},
	}

	cmd.Flags().StringVarP(&abbyfilePath, "file", "f", "", "Path to Abbyfile")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "./build", "Output directory for binaries")
	cmd.Flags().StringVar(&agentName, "agent", "", "Build a single agent by name")
	cmd.Flags().BoolVar(&pluginFlag, "plugin", false, "Also generate a Claude Code plugin directory")
	cmd.Flags().IntVar(&parallelism, "parallelism", 0, "Max concurrent agent builds (0 = sequential)")
	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Target runtime: auto, all, claude-code, codex, gemini")

	return cmd
}

func runBuild(abbyfilePath, outputDir, agentName string, pluginOutput bool, parallelism int, runtimeFlag string) error {
	if abbyfilePath == "" {
		abbyfilePath = resolveAbbyfile()
	}

	af, err := definition.ParseAbbyfile(abbyfilePath)
	if err != nil {
		return err
	}

	// Resolve base dir for relative .md paths.
	baseDir := filepath.Dir(abbyfilePath)
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
		// Use the Abbyfile key as the binary name, version from Abbyfile.
		def.Name = name
		def.Version = ref.Version
		defs[name] = def
	}

	if agentName != "" && len(defs) == 0 {
		return fmt.Errorf("agent %q not found in Abbyfile", agentName)
	}

	if err := builder.BuildAll(defs, cfg); err != nil {
		return err
	}

	// Generate MCP config for target runtimes.
	writers, err := runtimecfg.Resolve(runtimeFlag)
	if err != nil {
		return fmt.Errorf("resolving runtimes: %w", err)
	}

	absOut, _ := filepath.Abs(outputDir)
	entries := make(map[string]runtimecfg.ServerEntry)
	for name := range defs {
		entries[name] = runtimecfg.ServerEntry{
			Command: filepath.Join(absOut, name),
			Args:    []string{"serve-mcp"},
		}
	}

	for _, w := range writers {
		if err := w.Merge(w.LocalPath(), entries); err != nil {
			return fmt.Errorf("updating %s for %s: %w", w.LocalPath(), w.Runtime(), err)
		}
		fmt.Printf("Updated %s (%s)\n", w.LocalPath(), w.Runtime())
	}

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
