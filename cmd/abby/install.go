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
	var dryRun bool
	var configMethod string
	var envFlags []string
	var insecureSkipChecksum bool
	var force bool

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
			cfgOpts, err := configOptions(configMethod)
			if err != nil {
				return err
			}
			writers, err := runtimecfg.Resolve(runtimeFlag, cfgOpts)
			if err != nil {
				return err
			}
			env, err := parseEnvFlags(envFlags)
			if err != nil {
				return err
			}
			opts := installOptions{
				Global:       global,
				DryRun:       dryRun,
				SkipChecksum: insecureSkipChecksum,
				Force:        force,
				Writers:      writers,
				Env:          env,
				Out:          os.Stdout,
				Err:          os.Stderr,
			}
			// Print once per command invocation, not once per agent (a
			// bulk/multi install would otherwise repeat it per ref).
			warnProjectEnv(opts, scopeFor(global))

			// Validate --model is only used with single-agent installs.
			isBulk := allFlag || len(args) > 1
			if modelOverride != "" && isBulk {
				return fmt.Errorf("--model cannot be used with --all or multiple agents")
			}

			if allFlag {
				applied, err := runBulkInstall(args, opts)
				printSummary(opts.Out, applied, opts.DryRun)
				return err
			}

			if len(args) > 1 {
				applied, err := runMultiInstall(args, opts)
				printSummary(opts.Out, applied, opts.DryRun)
				return err
			}

			// Single agent install (original path).
			agentName, applied, err := installOne(args[0], opts)
			printSummary(opts.Out, applied, opts.DryRun)
			if err != nil {
				return err
			}

			return applyModelOverride(opts, agentName, modelOverride)
		},
	}

	cmd.Flags().BoolVarP(&global, "global", "g", false, "Install globally to /usr/local/bin")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override the agent's model in ~/.abbyfile/<name>/config.yaml")
	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Target runtime: auto, all, claude-code, codex, gemini")
	cmd.Flags().BoolVar(&allFlag, "all", false, "Install all agents from a repo (remote) or ./build/ (local)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show planned changes without installing anything")
	cmd.Flags().StringVar(&configMethod, "config-method", "", "auto (default; env ABBY_CONFIG_METHOD), cli, or file")
	cmd.Flags().StringArrayVar(&envFlags, "env", nil, "Set an environment variable for the MCP server (KEY=VALUE, repeatable)")
	cmd.Flags().BoolVar(&insecureSkipChecksum, "insecure-skip-checksum", false, "Skip release checksum verification (use with care)")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing .claude/agents/<name>.md that abby did not install or that was edited since")

	return cmd
}

// applyModelOverride sets (or, DryRun, previews) the agent's model override
// in ~/.abbyfile/<name>/config.yaml. A no-op when modelOverride is "".
func applyModelOverride(opts installOptions, agentName, modelOverride string) error {
	if modelOverride == "" {
		return nil
	}
	if opts.DryRun {
		fmt.Fprintf(opts.Out, "would set model override: %s → %s\n", agentName, modelOverride)
		return nil
	}
	if err := config.WriteField(agentName, "model", modelOverride); err != nil {
		return fmt.Errorf("writing model override: %w", err)
	}
	fmt.Fprintf(opts.Out, "Set model override: %s → %s\n", agentName, modelOverride)
	return nil
}

// installOne installs a single agent and returns its name and the applied changes.
func installOne(ref string, opts installOptions) (string, []appliedChange, error) {
	if github.IsRemoteRef(ref) {
		parsed, err := github.ParseRef(ref)
		if err != nil {
			return "", nil, err
		}
		applied, err := runRemoteInstall(ref, opts)
		if err != nil {
			return "", applied, err
		}
		return parsed.Agent, applied, nil
	}
	applied, err := runLocalInstall(ref, opts)
	if err != nil {
		return "", applied, err
	}
	return ref, applied, nil
}

// runBulkInstall handles --all for both local and remote installs.
func runBulkInstall(args []string, opts installOptions) ([]appliedChange, error) {
	if len(args) == 0 || !github.IsRemoteRef(args[0]) {
		return runBulkLocalInstall(opts)
	}
	return runBulkRemoteInstall(args[0], opts)
}

// runBulkLocalInstall installs all agent binaries from ./build/.
func runBulkLocalInstall(opts installOptions) ([]appliedChange, error) {
	entries, err := os.ReadDir("build")
	if err != nil {
		return nil, fmt.Errorf("reading build directory: %w (run 'abby build' first)", err)
	}

	var agents []string
	binaries := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "abby" || e.Name() == "publish" {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&0o111 == 0 {
			continue
		}
		agents = append(agents, e.Name())
		binaries[e.Name()] = true
	}
	agents = append(agents, builtFileOnlyAgents(binaries)...)

	if len(agents) == 0 {
		return nil, fmt.Errorf("no agent binaries or sub-agent files found in build/ (run 'abby build' first)")
	}

	fmt.Fprintf(opts.Out, "Installing %d agent(s) from ./build/...\n", len(agents))
	return installMany(agents, opts, false)
}

// runBulkRemoteInstall discovers and installs all agents from a GitHub repo.
func runBulkRemoteInstall(ref string, opts installOptions) ([]appliedChange, error) {
	parsed, err := github.ParseRef(ref)
	if err != nil {
		return nil, err
	}

	// --all doesn't make sense with an explicit agent name or version.
	if parsed.Agent != parsed.Repo {
		return nil, fmt.Errorf("--all requires a repo reference (github.com/owner/repo), not an agent reference")
	}
	if parsed.Version != "" {
		return nil, fmt.Errorf("--all cannot be used with a pinned version; each agent has its own version")
	}

	client := newGitHubClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	agents, err := client.ListAgents(ctx, parsed.Owner, parsed.Repo)
	if err != nil {
		return nil, fmt.Errorf("discovering agents: %w", err)
	}

	fmt.Fprintf(opts.Out, "Found %d agent(s) in %s/%s: %s\n", len(agents), parsed.Owner, parsed.Repo, strings.Join(agents, ", "))

	refs := make([]string, len(agents))
	for i, agent := range agents {
		refs[i] = fmt.Sprintf("github.com/%s/%s/%s", parsed.Owner, parsed.Repo, agent)
	}

	return installMany(refs, opts, true)
}

// runMultiInstall installs multiple explicitly-specified agents.
func runMultiInstall(args []string, opts installOptions) ([]appliedChange, error) {
	fmt.Fprintf(opts.Out, "Installing %d agent(s)...\n", len(args))
	return installMany(args, opts, false)
}

// installMany processes a list of refs, collecting errors and printing a summary.
func installMany(refs []string, opts installOptions, isRemote bool) ([]appliedChange, error) {
	var succeeded, failed int
	var errors []string
	var all []appliedChange

	for _, ref := range refs {
		var applied []appliedChange
		var err error
		if isRemote {
			applied, err = runRemoteInstall(ref, opts)
		} else if github.IsRemoteRef(ref) {
			applied, err = runRemoteInstall(ref, opts)
		} else {
			applied, err = runLocalInstall(ref, opts)
		}
		all = append(all, applied...)

		if err != nil {
			failed++
			name := ref
			if github.IsRemoteRef(ref) {
				if p, e := github.ParseRef(ref); e == nil {
					name = p.Agent
				}
			}
			errors = append(errors, fmt.Sprintf("  %s: %v", name, err))
			fmt.Fprintf(opts.Err, "Failed: %s: %v\n", name, err)
		} else {
			succeeded++
		}
	}

	verb, suffix := "Installed", ""
	if opts.DryRun {
		verb, suffix = "Would install", " (dry run)"
	}
	fmt.Fprintf(opts.Out, "\n%s %d/%d agent(s)%s", verb, succeeded, succeeded+failed, suffix)
	if failed > 0 {
		fmt.Fprintf(opts.Out, " (%d failed)\n", failed)
		return all, fmt.Errorf("%d agent(s) failed to install:\n%s", failed, strings.Join(errors, "\n"))
	}
	fmt.Fprintln(opts.Out)
	return all, nil
}

func runLocalInstall(name string, opts installOptions) ([]appliedChange, error) {
	src := filepath.Join("build", name)
	_, statErr := os.Stat(src)
	var m *agentManifest
	binVersion := ""
	if statErr == nil {
		m, _ = describeAgent(src)
		if m != nil {
			binVersion = m.Version
		}
	}
	// Check the sub-agent file before touching anything, so a refusal to
	// overwrite leaves no binary copied and no config written.
	fp, err := planAgentFile(name, binVersion, opts)
	if err != nil {
		return nil, err
	}
	if statErr != nil {
		if fp != nil {
			return installFileOnly(name, fp, opts)
		}
		return nil, fmt.Errorf("nothing to install for %q: neither %s nor %s exists (run 'abby build' first)", name, src, builtAgentFile(name))
	}

	binDir := opts.BinDir
	if binDir == "" {
		binDir = installBinDir(opts.Global)
	}
	dst := filepath.Join(binDir, name)
	scope := scopeFor(opts.Global)

	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path: %w", err)
	}

	entry := runtimecfg.ServerEntry{
		Command: absDst,
		Args:    []string{"serve-mcp"},
		Env:     opts.Env,
		Cwd:     cwdIfProject(scope, opts),
		Timeout: runtimeTimeout(m),
	}

	// Plan every writer's config change before touching the binary: a
	// planning failure (an unparsable existing config, or a --config-method
	// cli refusal) must leave no binary copied and no config written.
	planned, err := planEntries(opts, scope, map[string]runtimecfg.ServerEntry{name: entry})
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		fmt.Fprintf(opts.Out, "would install %s → %s\n", src, dst)
		if fp != nil {
			_ = writeAgentFile(fp, opts) // DryRun: prints only
		}
		return commitPlanned(opts, planned)
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating bin dir: %w", err)
	}
	if err := fsutil.CopyFile(src, dst); err != nil {
		return nil, fmt.Errorf("copying binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return nil, fmt.Errorf("setting permissions: %w", err)
	}
	fmt.Fprintf(opts.Out, "Installed %s → %s\n", name, dst)

	// Update MCP configs for target runtimes.
	applied, err := commitPlanned(opts, planned)
	if err != nil {
		return applied, err
	}

	tracked := registry.Entry{Name: name, Source: "local", Path: absDst, Scope: regScopeFor(opts.Global)}
	if m != nil {
		tracked.Version = m.Version
	}
	if fp != nil {
		if err := writeAgentFile(fp, opts); err != nil {
			return applied, err
		}
		tracked.AgentFile, tracked.AgentFileSHA256 = fp.Dst, fp.SHA256
	}
	if err := trackInstall(tracked); err != nil {
		return applied, err
	}
	return applied, nil
}

func runRemoteInstall(ref string, opts installOptions) ([]appliedChange, error) {
	parsed, err := github.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	// The agent name becomes the installed binary's file name. ParseRef only
	// validates an explicit agent segment (a defaulted one is the repo name,
	// which may contain dots), so check it here before anything is fetched.
	if !github.ValidAgentName(parsed.Agent) {
		return nil, fmt.Errorf("invalid agent name %q (repo names with dots need the agent named explicitly: github.com/%s/%s/<agent>)", parsed.Agent, parsed.Owner, parsed.Repo)
	}

	client := newGitHubClient()
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
		return nil, fmt.Errorf("resolving release: %w", err)
	}

	// Find asset for current platform.
	asset, err := github.FindAsset(release, parsed.Agent)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(opts.Out, "Downloading %s from %s...\n", asset.Name, release.TagName)

	// Download to temp file.
	tmpFile, err := os.CreateTemp("", "abbyfile-download-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := client.DownloadAsset(ctx, *asset, tmpFile); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("downloading: %w", err)
	}
	tmpFile.Close()

	// Verify the checksum before the downloaded binary is chmod'd executable
	// or run for --describe: a tampered binary must never execute.
	if err := verifyReleaseAsset(ctx, client, release, parsed.Agent, *asset, tmpPath, opts.SkipChecksum, opts.Err); err != nil {
		return nil, err
	}
	if !opts.SkipChecksum {
		fmt.Fprintf(opts.Out, "Checksum verified ✓\n")
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return nil, fmt.Errorf("setting permissions: %w", err)
	}

	// Verify it's a valid agent.
	manifest, err := describeAgent(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("downloaded binary is not a valid agent: %w", err)
	}
	fmt.Fprintf(opts.Out, "Verified: %s v%s\n", manifest.Name, manifest.Version)

	binDir := opts.BinDir
	if binDir == "" {
		binDir = installBinDir(opts.Global)
	}
	dst := filepath.Join(binDir, parsed.Agent)
	scope := scopeFor(opts.Global)

	absDst, err := filepath.Abs(dst)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path: %w", err)
	}
	entry := runtimecfg.ServerEntry{
		Command: absDst,
		Args:    []string{"serve-mcp"},
		Env:     opts.Env,
		Cwd:     cwdIfProject(scope, opts),
		Timeout: runtimeTimeout(manifest),
	}

	// Plan every writer's config change before touching the binary: a
	// planning failure must leave no binary copied and no config written.
	planned, err := planEntries(opts, scope, map[string]runtimecfg.ServerEntry{parsed.Agent: entry})
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		fmt.Fprintf(opts.Out, "would install %s → %s\n", asset.Name, dst)
		return commitPlanned(opts, planned)
	}

	// Move to install location.
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating bin dir: %w", err)
	}

	if err := fsutil.CopyFile(tmpPath, dst); err != nil {
		return nil, fmt.Errorf("installing binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return nil, fmt.Errorf("setting permissions: %w", err)
	}
	fmt.Fprintf(opts.Out, "Installed %s → %s\n", parsed.Agent, dst)

	// Wire MCP for target runtimes.
	applied, err := commitPlanned(opts, planned)
	if err != nil {
		return applied, err
	}

	// Track in registry.
	source := fmt.Sprintf("github.com/%s/%s/%s", parsed.Owner, parsed.Repo, parsed.Agent)
	if err := trackInstall(registry.Entry{Name: parsed.Agent, Source: source, Version: manifest.Version, Path: absDst, Scope: regScopeFor(opts.Global)}); err != nil {
		return applied, err
	}
	return applied, nil
}

// installBinDir returns the binary install directory.
// Binary location is abbyfile-internal, independent of runtime.
func installBinDir(global bool) string {
	if global {
		return "/usr/local/bin"
	}
	return filepath.Join(".abbyfile", "bin")
}

func trackInstall(e registry.Entry) error {
	regPath, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}
	reg.Set(e)
	return reg.Save()
}

// regScopeFor is the registry's scope label for an install.
func regScopeFor(global bool) string {
	if global {
		return "global"
	}
	return "local"
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
