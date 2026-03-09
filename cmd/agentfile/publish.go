package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/builder"
	"github.com/teabranch/agentfile/pkg/definition"
	"github.com/teabranch/agentfile/pkg/fsutil"
)

// crossTarget is a GOOS/GOARCH pair for cross-compilation.
type crossTarget struct {
	OS   string
	Arch string
}

var defaultTargets = []crossTarget{
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
}

func newPublishCommand() *cobra.Command {
	var (
		agentfilePath string
		agentName     string
		dryRun        bool
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Cross-compile and publish agents to GitHub Releases",
		Long: `Builds agent binaries for multiple platforms and creates a GitHub Release
using the gh CLI. The release tag follows the format <agent>/v<version>.

Requires the gh CLI to be installed and authenticated.
Use --dry-run to cross-compile without creating a release.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPublish(agentfilePath, agentName, dryRun)
		},
	}

	cmd.Flags().StringVarP(&agentfilePath, "file", "f", "", "Path to Agentfile")
	cmd.Flags().StringVar(&agentName, "agent", "", "Publish a single agent by name")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Cross-compile only, skip GitHub Release creation")

	return cmd
}

func runPublish(agentfilePath, agentName string, dryRun bool) error {
	if agentfilePath == "" {
		agentfilePath = resolveAgentfile()
	}

	// Verify gh CLI is available (unless dry-run).
	if !dryRun {
		if _, err := exec.LookPath("gh"); err != nil {
			return fmt.Errorf("gh CLI not found on PATH (install: https://cli.github.com)")
		}
	}

	af, err := definition.ParseAgentfile(agentfilePath)
	if err != nil {
		return err
	}

	baseDir := filepath.Dir(agentfilePath)
	if !filepath.IsAbs(baseDir) {
		cwd, _ := os.Getwd()
		baseDir = filepath.Join(cwd, baseDir)
	}

	moduleDir := builder.DetectModuleDir()

	// Build output goes into a publish-specific directory.
	publishDir := filepath.Join("build", "publish")
	if err := os.MkdirAll(publishDir, 0o755); err != nil {
		return fmt.Errorf("creating publish dir: %w", err)
	}

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
		def.Name = name
		def.Version = ref.Version

		// Determine cross-compilation targets: prefer Agentfile publish config, fall back to defaults.
		targets := defaultTargets
		if af.Publish != nil && len(af.Publish.Targets) > 0 {
			targets = nil
			for _, pt := range af.Publish.Targets {
				targets = append(targets, crossTarget{OS: pt.OS, Arch: pt.Arch})
			}
		}

		// Cross-compile for all targets.
		var assetPaths []string
		for _, target := range targets {
			fmt.Fprintf(os.Stderr, "Building %s for %s/%s...\n", name, target.OS, target.Arch)

			cfg := builder.BuildConfig{
				OutputDir:  publishDir,
				ModuleDir:  moduleDir,
				TargetOS:   target.OS,
				TargetArch: target.Arch,
			}
			if err := builder.Build(def, cfg); err != nil {
				return fmt.Errorf("building %s for %s/%s: %w", name, target.OS, target.Arch, err)
			}

			assetPath := filepath.Join(publishDir, fmt.Sprintf("%s-%s-%s", name, target.OS, target.Arch))
			assetPaths = append(assetPaths, assetPath)
		}

		// Generate SHA256 checksums file.
		var checksumLines []string
		for _, ap := range assetPaths {
			hash, hashErr := fsutil.SHA256File(ap)
			if hashErr != nil {
				return fmt.Errorf("computing checksum for %s: %w", ap, hashErr)
			}
			checksumLines = append(checksumLines, fmt.Sprintf("%s  %s", hash, filepath.Base(ap)))
		}
		sumsPath := filepath.Join(publishDir, name+"-sha256sums.txt")
		if err := os.WriteFile(sumsPath, []byte(strings.Join(checksumLines, "\n")+"\n"), 0o644); err != nil {
			return fmt.Errorf("writing checksums: %w", err)
		}
		assetPaths = append(assetPaths, sumsPath)

		if dryRun {
			fmt.Printf("Dry run: built %d binaries for %s v%s in %s\n",
				len(assetPaths)-1, name, ref.Version, publishDir)
			continue
		}

		// Create GitHub Release via gh CLI.
		tag := fmt.Sprintf("%s/v%s", name, ref.Version)
		title := fmt.Sprintf("%s v%s", name, ref.Version)

		ghArgs := []string{"release", "create", tag, "--title", title, "--generate-notes"}
		ghArgs = append(ghArgs, assetPaths...)

		fmt.Fprintf(os.Stderr, "Creating release %s...\n", tag)
		ghCmd := exec.Command("gh", ghArgs...)
		ghCmd.Stdout = os.Stdout
		ghCmd.Stderr = os.Stderr
		if err := ghCmd.Run(); err != nil {
			return fmt.Errorf("creating release %s: %w", tag, err)
		}
		fmt.Printf("Published %s v%s\n", name, ref.Version)
	}

	if agentName != "" {
		found := false
		for name := range af.Agents {
			if name == agentName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("agent %q not found in Agentfile", agentName)
		}
	}

	return nil
}
