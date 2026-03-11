package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/config"
	"github.com/teabranch/agentfile/pkg/fsutil"
	"github.com/teabranch/agentfile/pkg/github"
	"github.com/teabranch/agentfile/pkg/registry"
)

func newInstallCommand() *cobra.Command {
	var global bool
	var modelOverride string

	cmd := &cobra.Command{
		Use:   "install <agent-name | github.com/owner/repo[/agent][@version]>",
		Short: "Install an agent binary (local or remote)",
		Long: `Installs an agent binary and updates the MCP config.

Local install (from ./build/):
  agentfile install my-agent

Remote install (from GitHub Releases):
  agentfile install github.com/owner/repo/agent
  agentfile install github.com/owner/repo/agent@1.0.0

By default, installs to .agentfile/bin/ (project-local) and updates .mcp.json.
With --global, installs to /usr/local/bin/ and updates ~/.claude/mcp.json.

Override settings at install time:
  agentfile install --model gpt-5 github.com/owner/repo/agent`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var agentName string
			if github.IsRemoteRef(args[0]) {
				parsed, err := github.ParseRef(args[0])
				if err != nil {
					return err
				}
				agentName = parsed.Agent
				if err := runRemoteInstall(args[0], global); err != nil {
					return err
				}
			} else {
				agentName = args[0]
				if err := runLocalInstall(args[0], global); err != nil {
					return err
				}
			}

			// Write config override if --model was specified.
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
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the agent's model in ~/.agentfile/<name>/config.yaml")

	return cmd
}

func runLocalInstall(name string, global bool) error {
	src := filepath.Join("build", name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("binary not found: %s (run 'agentfile build' first)", src)
	}

	binDir, mcpPath := installPaths(global)

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

	// Update mcp.json.
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}
	entries := map[string]MCPServerEntry{
		name: {
			Command: absDst,
			Args:    []string{"serve-mcp"},
		},
	}
	if err := mergeMCPJSON(mcpPath, entries); err != nil {
		return fmt.Errorf("updating %s: %w", mcpPath, err)
	}
	fmt.Printf("Updated %s\n", mcpPath)

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

func runRemoteInstall(ref string, global bool) error {
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
	tmpFile, err := os.CreateTemp("", "agentfile-download-*")
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
		sumsFile, sErr := os.CreateTemp("", "agentfile-sums-*")
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
	binDir, mcpPath := installPaths(global)
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

	// Wire MCP.
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}
	entries := map[string]MCPServerEntry{
		parsed.Agent: {
			Command: absDst,
			Args:    []string{"serve-mcp"},
		},
	}
	if err := mergeMCPJSON(mcpPath, entries); err != nil {
		return fmt.Errorf("updating %s: %w", mcpPath, err)
	}
	fmt.Printf("Updated %s\n", mcpPath)

	// Track in registry.
	source := fmt.Sprintf("github.com/%s/%s/%s", parsed.Owner, parsed.Repo, parsed.Agent)
	scope := "local"
	if global {
		scope = "global"
	}
	return trackInstall(parsed.Agent, source, manifest.Version, absDst, scope)
}

func installPaths(global bool) (binDir, mcpPath string) {
	if global {
		binDir = "/usr/local/bin"
		home, _ := os.UserHomeDir()
		mcpPath = filepath.Join(home, ".claude", "mcp.json")
	} else {
		binDir = filepath.Join(".agentfile", "bin")
		mcpPath = ".mcp.json"
	}
	return
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
