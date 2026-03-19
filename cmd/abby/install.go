package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/fsutil"
	"github.com/teabranch/abbyfile/pkg/github"
	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func newInstallCommand() *cobra.Command {
	var global bool
	var modelOverride string
	var runtimeFlag string
	var allFlag bool

	cmd := &cobra.Command{
		Use:   "install [flags] <ref>...",
		Short: "Install agent binaries (local or remote)",
		Long: `Installs agent binaries and updates the MCP config for detected runtimes.

Local install (from ./build/):
  abby install my-agent

Remote install (from GitHub Releases):
  abby install github.com/owner/repo/agent
  abby install github.com/owner/repo/agent@1.0.0

Bulk install (multiple agents):
  abby install --all github.com/owner/repo         # all agents from a repo
  abby install --all                                # all agents from ./build/
  abby install github.com/o/r/a1 github.com/o/r/a2 # specific agents (any repos)

By default, installs to .abbyfile/bin/ (project-local) and updates MCP config.
With --global, installs to /usr/local/bin/ and updates global MCP config.

Override settings at install time:
  abby install --model gpt-5 github.com/owner/repo/agent
  abby install --runtime codex github.com/owner/repo/agent`,
		Args: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			if all {
				if len(args) > 1 {
					return fmt.Errorf("--all accepts at most one repo reference (or none for local)")
				}
				return nil
			}
			if len(args) < 1 {
				return fmt.Errorf("requires at least 1 arg(s)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			writers, err := runtimecfg.Resolve(runtimeFlag)
			if err != nil {
				return err
			}

			// Validate --model is only used with single-agent installs.
			isBulk := allFlag || len(args) > 1
			if modelOverride != "" && isBulk {
				return fmt.Errorf("--model cannot be used with --all or multiple agents")
			}

			if allFlag {
				return runBulkInstall(args, global, writers)
			}

			if len(args) > 1 {
				return runMultiInstall(args, global, writers)
			}

			// Single agent install (original path).
			agentName, err := installOne(args[0], global, writers)
			if err != nil {
				return err
			}

			if modelOverride != "" {
				if err := config.WriteField(agentName, "model", modelOverride); err != nil {
					return fmt.Errorf("writing model override: %w", err)
				}
				fmt.Printf("Set model override: %s → %s\n", agentName, modelOverride)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&global, "global", "g", false, "Install globally to /usr/local/bin")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the agent's model in ~/.abbyfile/<name>/config.yaml")
	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Target runtime: auto, all, claude-code, codex, gemini")
	cmd.Flags().BoolVar(&allFlag, "all", false, "Install all agents from a repo (remote) or ./build/ (local)")

	return cmd
}

// installOne installs a single agent and returns its name.
func installOne(ref string, global bool, writers []runtimecfg.ConfigWriter) (string, error) {
	if github.IsRemoteRef(ref) {
		parsed, err := github.ParseRef(ref)
		if err != nil {
			return "", err
		}
		if err := runRemoteInstall(ref, global, writers); err != nil {
			return "", err
		}
		return parsed.Agent, nil
	}
	if err := runLocalInstall(ref, global, writers); err != nil {
		return "", err
	}
	return ref, nil
}

// runBulkInstall handles --all for both local and remote installs.
func runBulkInstall(args []string, global bool, writers []runtimecfg.ConfigWriter) error {
	if len(args) == 0 || !github.IsRemoteRef(args[0]) {
		return runBulkLocalInstall(global, writers)
	}
	return runBulkRemoteInstall(args[0], global, writers)
}

// runBulkLocalInstall installs all agent binaries from ./build/.
func runBulkLocalInstall(global bool, writers []runtimecfg.ConfigWriter) error {
	entries, err := os.ReadDir("build")
	if err != nil {
		return fmt.Errorf("reading build directory: %w (run 'abby build' first)", err)
	}

	var agents []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "abby" || e.Name() == "publish" {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&0o111 == 0 {
			continue
		}
		agents = append(agents, e.Name())
	}

	if len(agents) == 0 {
		return fmt.Errorf("no agent binaries found in build/ (run 'abby build' first)")
	}

	fmt.Printf("Installing %d agent(s) from ./build/...\n", len(agents))
	return installMany(agents, global, writers, false)
}

// runBulkRemoteInstall discovers and installs all agents from a GitHub repo.
func runBulkRemoteInstall(ref string, global bool, writers []runtimecfg.ConfigWriter) error {
	parsed, err := github.ParseRef(ref)
	if err != nil {
		return err
	}

	// --all doesn't make sense with an explicit agent name or version.
	if parsed.Agent != parsed.Repo {
		return fmt.Errorf("--all requires a repo reference (github.com/owner/repo), not an agent reference")
	}
	if parsed.Version != "" {
		return fmt.Errorf("--all cannot be used with a pinned version; each agent has its own version")
	}

	client := github.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	agents, err := client.ListAgents(ctx, parsed.Owner, parsed.Repo)
	if err != nil {
		return fmt.Errorf("discovering agents: %w", err)
	}

	fmt.Printf("Found %d agent(s) in %s/%s: %s\n", len(agents), parsed.Owner, parsed.Repo, strings.Join(agents, ", "))

	refs := make([]string, len(agents))
	for i, agent := range agents {
		refs[i] = fmt.Sprintf("github.com/%s/%s/%s", parsed.Owner, parsed.Repo, agent)
	}

	return installMany(refs, global, writers, true)
}

// runMultiInstall installs multiple explicitly-specified agents.
func runMultiInstall(args []string, global bool, writers []runtimecfg.ConfigWriter) error {
	fmt.Printf("Installing %d agent(s)...\n", len(args))
	return installMany(args, global, writers, false)
}

// installMany processes a list of refs, collecting errors and printing a summary.
func installMany(refs []string, global bool, writers []runtimecfg.ConfigWriter, isRemote bool) error {
	var succeeded, failed int
	var errors []string

	for _, ref := range refs {
		var err error
		if isRemote {
			err = runRemoteInstall(ref, global, writers)
		} else if github.IsRemoteRef(ref) {
			err = runRemoteInstall(ref, global, writers)
		} else {
			err = runLocalInstall(ref, global, writers)
		}

		if err != nil {
			failed++
			name := ref
			if github.IsRemoteRef(ref) {
				if p, e := github.ParseRef(ref); e == nil {
					name = p.Agent
				}
			}
			errors = append(errors, fmt.Sprintf("  %s: %v", name, err))
			fmt.Fprintf(os.Stderr, "Failed: %s: %v\n", name, err)
		} else {
			succeeded++
		}
	}

	fmt.Printf("\nInstalled %d/%d agent(s)", succeeded, succeeded+failed)
	if failed > 0 {
		fmt.Printf(" (%d failed)\n", failed)
		return fmt.Errorf("%d agent(s) failed to install:\n%s", failed, strings.Join(errors, "\n"))
	}
	fmt.Println()
	return nil
}

func runLocalInstall(name string, global bool, writers []runtimecfg.ConfigWriter) error {
	src := filepath.Join("build", name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("binary not found: %s (run 'abby build' first)", src)
	}

	binDir := installBinDir(global)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("creating bin dir: %w", err)
	}

	dst := filepath.Join(binDir, name)
	if err := fsutil.CopyFile(src, dst); err != nil {
		return fmt.Errorf("copying binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}
	fmt.Printf("Installed %s → %s\n", name, dst)

	// Update MCP configs for target runtimes.
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}
	entries := map[string]runtimecfg.ServerEntry{
		name: {
			Command: absDst,
			Args:    []string{"serve-mcp"},
		},
	}
	if err := mergeRuntimeConfigs(writers, global, entries); err != nil {
		return err
	}

	// Track in registry.
	version := ""
	if m, err := describeAgent(absDst); err == nil {
		version = m.Version
	}
	scope := "local"
	if global {
		scope = "global"
	}
	return trackInstall(name, "local", version, absDst, scope)
}

func runRemoteInstall(ref string, global bool, writers []runtimecfg.ConfigWriter) error {
	parsed, err := github.ParseRef(ref)
	if err != nil {
		return err
	}

	client := github.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Resolve release.
	var release *github.Release
	if parsed.Version != "" {
		release, err = client.GetRelease(ctx, parsed)
	} else {
		release, err = client.LatestRelease(ctx, parsed)
	}
	if err != nil {
		return fmt.Errorf("resolving release: %w", err)
	}

	// Find asset for current platform.
	asset, err := github.FindAsset(release, parsed.Agent)
	if err != nil {
		return err
	}

	fmt.Printf("Downloading %s from %s...\n", asset.Name, release.TagName)

	// Download to temp file.
	tmpFile, err := os.CreateTemp("", "abbyfile-download-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := client.DownloadAsset(ctx, *asset, tmpFile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("downloading: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	// Verify it's a valid agent.
	manifest, err := describeAgent(tmpPath)
	if err != nil {
		return fmt.Errorf("downloaded binary is not a valid agent: %w", err)
	}
	fmt.Printf("Verified: %s v%s\n", manifest.Name, manifest.Version)

	// Verify checksum if a checksums file exists in the release.
	if sumsAsset := findChecksumAsset(release, parsed.Agent); sumsAsset != nil {
		fmt.Printf("Verifying checksum...\n")
		sumsFile, sErr := os.CreateTemp("", "abbyfile-sums-*")
		if sErr == nil {
			if sErr = client.DownloadAsset(ctx, *sumsAsset, sumsFile); sErr == nil {
				sumsFile.Close()
				sumsData, _ := os.ReadFile(sumsFile.Name())
				sums := github.ParseChecksumFile(string(sumsData))
				if expected, ok := sums[asset.Name]; ok {
					if vErr := github.VerifyChecksum(tmpPath, expected); vErr != nil {
						os.Remove(sumsFile.Name())
						return fmt.Errorf("checksum verification failed: %w", vErr)
					}
					fmt.Printf("Checksum verified ✓\n")
				}
			}
			os.Remove(sumsFile.Name())
		}
	}

	// Move to install location.
	binDir := installBinDir(global)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("creating bin dir: %w", err)
	}

	dst := filepath.Join(binDir, parsed.Agent)
	if err := fsutil.CopyFile(tmpPath, dst); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}
	fmt.Printf("Installed %s → %s\n", parsed.Agent, dst)

	// Wire MCP for target runtimes.
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}
	entries := map[string]runtimecfg.ServerEntry{
		parsed.Agent: {
			Command: absDst,
			Args:    []string{"serve-mcp"},
		},
	}
	if err := mergeRuntimeConfigs(writers, global, entries); err != nil {
		return err
	}

	// Track in registry.
	source := fmt.Sprintf("github.com/%s/%s/%s", parsed.Owner, parsed.Repo, parsed.Agent)
	scope := "local"
	if global {
		scope = "global"
	}
	return trackInstall(parsed.Agent, source, manifest.Version, absDst, scope)
}

// installBinDir returns the binary install directory.
// Binary location is abbyfile-internal, independent of runtime.
func installBinDir(global bool) string {
	if global {
		return "/usr/local/bin"
	}
	return filepath.Join(".abbyfile", "bin")
}

// mergeRuntimeConfigs writes MCP server entries to all target runtime configs.
func mergeRuntimeConfigs(writers []runtimecfg.ConfigWriter, global bool, entries map[string]runtimecfg.ServerEntry) error {
	for _, w := range writers {
		var cfgPath string
		if global {
			var err error
			cfgPath, err = w.GlobalPath()
			if err != nil {
				return fmt.Errorf("resolving global path for %s: %w", w.Runtime(), err)
			}
		} else {
			cfgPath = w.LocalPath()
		}
		if err := w.Merge(cfgPath, entries); err != nil {
			return fmt.Errorf("updating %s for %s: %w", cfgPath, w.Runtime(), err)
		}
		fmt.Printf("Updated %s (%s)\n", cfgPath, w.Runtime())
	}
	return nil
}

func trackInstall(name, source, version, path, scope string) error {
	regPath, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}
	reg.Set(registry.Entry{
		Name:    name,
		Source:  source,
		Version: version,
		Path:    path,
		Scope:   scope,
	})
	return reg.Save()
}

// findChecksumAsset looks for a SHA256SUMS file in the release assets.
func findChecksumAsset(release *github.Release, agentName string) *github.Asset {
	for _, name := range []string{
		agentName + "-sha256sums.txt",
		"SHA256SUMS",
		"checksums.txt",
	} {
		for i := range release.Assets {
			if release.Assets[i].Name == name {
				return &release.Assets[i]
			}
		}
	}
	return nil
}
