package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

type checkStatus int

const (
	statusOK checkStatus = iota
	statusWarn
	statusFail
)

type check struct {
	Status checkStatus
	Text   string
}

// legacyEraVersion is the pre-2026-07-28 protocol, negotiated via initialize.
const legacyEraVersion = "2025-11-25"

const doctorHandshakeTimeout = 10 * time.Second

type doctorDeps struct {
	describe   func(bin, dir string) (*agentManifest, error)
	handshake  func(ctx context.Context, bin, dir, protocolVersion string) (string, error)
	writers    []runtimecfg.ConfigWriter
	legacyPath func() (string, error)
}

func newDoctorCommand() *cobra.Command {
	var runtimeFlag, methodFlag string
	cmd := &cobra.Command{
		Use:   "doctor [agent]...",
		Short: "Diagnose installed agents: binary, runtime config entries, MCP handshake, sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgOpts, err := configOptions(methodFlag)
			if err != nil {
				return err
			}
			writers, err := runtimecfg.Resolve(runtimeFlag, cfgOpts)
			if err != nil {
				return err
			}
			regPath, err := registry.DefaultPath()
			if err != nil {
				return err
			}
			reg, err := registry.Load(regPath)
			if err != nil {
				return err
			}
			entries, err := doctorTargets(reg, args)
			if err != nil {
				return err
			}
			deps := doctorDeps{describe: describeAgentIn, handshake: mcpHandshake, legacyPath: runtimecfg.LegacyClaudePath}
			_ = writers // validates --runtime early
			failed := false
			for _, e := range entries {
				perEntry := cfgOpts
				perEntry.ProjectRoot = projectRootFor(e)
				if deps.writers, err = runtimecfg.Resolve(runtimeFlag, perEntry); err != nil {
					return err
				}
				if printChecks(cmd.OutOrStdout(), fmt.Sprintf("%s (v%s, %s, %s)", e.Name, e.Version, e.Scope, e.Path), diagnoseAgent(e, deps)) {
					failed = true
				}
			}
			if cs := legacyChecks(deps); len(cs) > 0 {
				printChecks(cmd.OutOrStdout(), "legacy config", cs)
			}
			if failed {
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runtimeFlag, "runtime", "auto", "Runtimes to check: auto, all, claude-code, codex, gemini")
	cmd.Flags().StringVar(&methodFlag, "config-method", "", "Accepted for symmetry; doctor only reads config")
	return cmd
}

// doctorTargets returns the named registry entries, or all of them.
func doctorTargets(reg *registry.Registry, names []string) ([]registry.Entry, error) {
	if len(names) == 0 {
		all := reg.List()
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		if len(all) == 0 {
			return nil, fmt.Errorf("no agents installed (see `abby install`)")
		}
		return all, nil
	}
	var out []registry.Entry
	for _, n := range names {
		e, ok := reg.Get(n)
		if !ok {
			return nil, fmt.Errorf("agent %q is not installed", n)
		}
		out = append(out, e)
	}
	return out, nil
}

func diagnoseAgent(e registry.Entry, d doctorDeps) []check {
	var cs []check
	fi, err := os.Stat(e.Path)
	if err != nil || fi.Mode()&0o111 == 0 {
		return []check{{statusFail, fmt.Sprintf("binary %s is missing or not executable — reinstall with `abby install`", e.Path)}}
	}
	cs = append(cs, check{statusOK, "binary " + e.Path})

	// dir is the agent's own project root (its install directory, from the
	// registry entry's layout), so --describe and the MCP handshake run
	// there rather than in doctor's own cwd: a sandbox with a relative
	// allowedDirs entry (e.g. ".") must report the project's directory, not
	// wherever `abby doctor` happened to be invoked from. "" (global/unknown
	// layouts) leaves the spawned process's cwd unset.
	dir := projectRootFor(e)

	var m *agentManifest
	if d.describe != nil {
		if m, err = d.describe(e.Path, dir); err != nil {
			cs = append(cs, check{statusFail, fmt.Sprintf("--describe failed: %v", err)})
		} else if m.Sandbox != nil {
			sb := m.Sandbox
			cmds, _ := json.Marshal(sb.AllowCommands)
			cs = append(cs, check{statusOK, fmt.Sprintf("sandbox: bash=%s allow_commands=%s allowed_dirs=%s", sb.Bash, cmds, strings.Join(sb.AllowedDirs, ", "))})
			for _, w := range sb.Warnings {
				cs = append(cs, check{statusWarn, "sandbox: " + w})
			}
			if sb.Bash == "unrestricted" && len(sb.Warnings) == 0 {
				cs = append(cs, check{statusWarn, "sandbox: bash is unrestricted"})
			}
		}
	}

	if d.handshake != nil {
		for _, pv := range []string{"", legacyEraVersion} {
			ctx, cancel := context.WithTimeout(context.Background(), doctorHandshakeTimeout)
			got, err := d.handshake(ctx, e.Path, dir, pv)
			cancel()
			label := "server/discover"
			if pv != "" {
				label = "initialize"
			}
			if err != nil {
				cs = append(cs, check{statusFail, fmt.Sprintf("serve-mcp handshake (%s) failed: %v", label, err)})
			} else {
				cs = append(cs, check{statusOK, fmt.Sprintf("serve-mcp speaks %s (%s)", got, label)})
			}
		}
	}

	scope := scopeFor(e.Scope == "global")
	want := runtimeTimeout(m)
	for _, w := range d.writers {
		path, _ := w.ConfigPath(scope)
		entry, ok, err := w.Lookup(scope, e.Name)
		switch {
		case err != nil:
			cs = append(cs, check{statusFail, fmt.Sprintf("%s %s: %v", w.Runtime(), path, err)})
		case !ok:
			cs = append(cs, check{statusWarn, fmt.Sprintf("%s %s: no entry for %s — run `abby install --runtime %s %s`", w.Runtime(), path, e.Name, w.Runtime(), e.Name)})
		case entry.Command != e.Path:
			cs = append(cs, check{statusFail, fmt.Sprintf("%s %s: entry points at %s, not %s — reinstall", w.Runtime(), path, entry.Command, e.Path)})
		default:
			cs = append(cs, check{statusOK, fmt.Sprintf("%s %s → %s", w.Runtime(), path, entry.Command)})
		}
		if err != nil || !ok {
			continue
		}
		switch {
		case want > 0 && entry.Timeout > 0 && entry.Timeout < want:
			cs = append(cs, check{statusWarn, fmt.Sprintf("%s timeout is %s but the agent may run for up to %s — reinstall to refresh it", w.Runtime(), entry.Timeout, want)})
		case want > runtimecfg.CodexDefaultToolTimeout && entry.Timeout == 0 && w.Runtime() == runtimecfg.Codex:
			cs = append(cs, check{statusWarn, fmt.Sprintf("%s has no tool_timeout_sec (Codex default %ds) but the agent may run for up to %s — reinstall to set it", w.Runtime(), int(runtimecfg.CodexDefaultToolTimeout.Seconds()), want)})
		}
		if w.Runtime() == runtimecfg.Codex && scope == runtimecfg.ScopeProject {
			cs = append(cs, check{statusWarn, "Codex loads project .codex/config.toml only in trusted projects"})
		}
	}
	return cs
}

// legacyChecks reports entries in ~/.claude/mcp.json (written by abby <= v0.11; never read by Claude Code).
func legacyChecks(d doctorDeps) []check {
	if d.legacyPath == nil {
		return nil
	}
	p, err := d.legacyPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &doc) != nil || len(doc.MCPServers) == 0 {
		return nil
	}
	names := make([]string, 0, len(doc.MCPServers))
	for n := range doc.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	return []check{{statusWarn, fmt.Sprintf("%s has entries %s written by abby < v0.12; Claude Code never reads this file — reinstall them with `abby install --global`, then delete %s", p, strings.Join(names, ", "), p)}}
}

func printChecks(w io.Writer, title string, cs []check) (failed bool) {
	fmt.Fprintln(w, title)
	for _, c := range cs {
		mark := "✓"
		switch c.Status {
		case statusWarn:
			mark = "!"
		case statusFail:
			mark = "✗"
			failed = true
		}
		fmt.Fprintf(w, "  %s %s\n", mark, c.Text)
	}
	return failed
}

// mcpHandshake connects to `bin serve-mcp` — run with its working directory
// set to dir when non-empty, so a sandbox with a relative allowedDirs entry
// resolves against the agent's own project root, not doctor's cwd — and
// returns the negotiated protocol version.
func mcpHandshake(ctx context.Context, bin, dir, protocolVersion string) (string, error) {
	client := gomcp.NewClient(&gomcp.Implementation{Name: "abby-doctor", Version: cliVersion}, nil)
	cmd := exec.Command(bin, "serve-mcp")
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stderr = io.Discard // the agent's own startup logs would clutter doctor output
	sess, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, &gomcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		return "", err
	}
	defer sess.Close()
	return sess.InitializeResult().ProtocolVersion, nil
}
