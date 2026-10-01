package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func textOf(cs []check) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Text + "\n")
	}
	return b.String()
}

func TestDiagnoseAgentHealthy(t *testing.T) {
	d := chdir(t)
	bin := filepath.Join(d, "agent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	w := fileWriters(runtimecfg.ClaudeCode)[0]
	c, _ := w.PlanAdd(runtimecfg.ScopeProject, "agent", runtimecfg.ServerEntry{Command: bin, Args: []string{"serve-mcp"}, Timeout: 40 * time.Second})
	c.Apply()
	deps := doctorDeps{
		describe: func(string, string) (*agentManifest, error) {
			return &agentManifest{Name: "agent", Version: "1.0.0", Sandbox: &manifestSandbox{Bash: "restricted", AllowCommands: []string{"go test ./..."}, AllowedDirs: []string{d}}}, nil
		},
		handshake: func(_ context.Context, _, _, pv string) (string, error) {
			if pv == "" {
				return "2026-07-28", nil
			}
			return pv, nil
		},
		writers: []runtimecfg.ConfigWriter{w},
	}
	cs := diagnoseAgent(registry.Entry{Name: "agent", Path: bin, Scope: "local", Version: "1.0.0"}, deps)
	for _, c := range cs {
		if c.Status == statusFail {
			t.Errorf("unexpected failure: %s", c.Text)
		}
	}
	s := textOf(cs)
	for _, want := range []string{"2026-07-28", "2025-11-25", "restricted", "go test ./...", ".mcp.json"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestDiagnoseAgentProblems(t *testing.T) {
	d := chdir(t)
	bin := filepath.Join(d, "agent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	w := fileWriters(runtimecfg.ClaudeCode)[0]
	c, _ := w.PlanAdd(runtimecfg.ScopeProject, "agent", runtimecfg.ServerEntry{Command: "/elsewhere/agent", Args: []string{"serve-mcp"}, Timeout: 20 * time.Second})
	c.Apply()
	deps := doctorDeps{
		describe: func(string, string) (*agentManifest, error) {
			return &agentManifest{Name: "agent", ToolTimeout: "30s", Sandbox: &manifestSandbox{Bash: "unrestricted", Warnings: []string{"sandbox.bash is unrestricted"}}}, nil
		},
		handshake: func(context.Context, string, string, string) (string, error) {
			return "", errors.New("connection closed")
		},
		writers: []runtimecfg.ConfigWriter{w},
	}
	cs := diagnoseAgent(registry.Entry{Name: "agent", Path: bin, Scope: "local"}, deps)
	s := textOf(cs)
	var fails, warns int
	for _, c := range cs {
		switch c.Status {
		case statusFail:
			fails++
		case statusWarn:
			warns++
		}
	}
	if fails < 2 || warns < 2 {
		t.Errorf("fails=%d warns=%d:\n%s", fails, warns, s)
	}
	for _, want := range []string{"/elsewhere/agent", "connection closed", "unrestricted", "40s"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

// Fix round 1 (controller ruling): diagnoseAgent must run --describe and the
// MCP handshake in the agent's own project root, not doctor's cwd, so a
// sandbox with a relative allowedDirs entry (e.g. ".") reports the project
// it's installed in rather than wherever `abby doctor` was invoked from.
func TestDiagnoseAgentPassesProjectRootAsDir(t *testing.T) {
	d := chdir(t)
	binDir := filepath.Join(d, ".abbyfile", "bin")
	os.MkdirAll(binDir, 0o755)
	bin := filepath.Join(binDir, "agent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)

	var describeDir, handshakeDir string
	deps := doctorDeps{
		describe: func(_, dir string) (*agentManifest, error) {
			describeDir = dir
			return &agentManifest{Name: "agent"}, nil
		},
		handshake: func(_ context.Context, _, dir, pv string) (string, error) {
			handshakeDir = dir
			return "2026-07-28", nil
		},
	}
	diagnoseAgent(registry.Entry{Name: "agent", Path: bin, Scope: "local"}, deps)
	if describeDir != d || handshakeDir != d {
		t.Errorf("local entry: describeDir=%q handshakeDir=%q, want %q", describeDir, handshakeDir, d)
	}

	// A global entry (or any layout projectRootFor doesn't recognise) has no
	// project root: dir must be "" so the spawned process just inherits
	// doctor's own cwd, as it did before this fix.
	describeDir, handshakeDir = "not-called", "not-called"
	globalBin := filepath.Join(d, "usr-local-bin", "agent")
	os.MkdirAll(filepath.Dir(globalBin), 0o755)
	os.WriteFile(globalBin, []byte("#!/bin/sh\n"), 0o755)
	diagnoseAgent(registry.Entry{Name: "agent", Path: globalBin, Scope: "global"}, deps)
	if describeDir != "" || handshakeDir != "" {
		t.Errorf("global entry: describeDir=%q handshakeDir=%q, want \"\"", describeDir, handshakeDir)
	}
}

func TestDiagnoseMissingBinary(t *testing.T) {
	chdir(t)
	cs := diagnoseAgent(registry.Entry{Name: "agent", Path: "/nope/agent", Scope: "local"}, doctorDeps{})
	if len(cs) == 0 || cs[0].Status != statusFail {
		t.Fatalf("checks = %+v", cs)
	}
}

func TestLegacyChecks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":{"old-agent":{"command":"/x"}}}`), 0o644)
	cs := legacyChecks(doctorDeps{legacyPath: func() (string, error) { return p, nil }})
	if len(cs) != 1 || cs[0].Status != statusWarn || !strings.Contains(cs[0].Text, "old-agent") || !strings.Contains(cs[0].Text, p) {
		t.Errorf("legacy = %+v", cs)
	}
	if cs := legacyChecks(doctorDeps{legacyPath: func() (string, error) { return filepath.Join(t.TempDir(), "none"), nil }}); len(cs) != 0 {
		t.Errorf("no legacy file → no checks, got %+v", cs)
	}
}

func TestPrintChecks(t *testing.T) {
	var out bytes.Buffer
	failed := printChecks(&out, "agent", []check{{statusOK, "a"}, {statusWarn, "b"}, {statusFail, "c"}})
	if !failed || !strings.Contains(out.String(), "✓ a") || !strings.Contains(out.String(), "! b") || !strings.Contains(out.String(), "✗ c") {
		t.Errorf("failed=%v out=%q", failed, out.String())
	}
}

// A Codex entry with no tool_timeout_sec gets Codex's 60s default, which can
// be shorter than the agent's own limit; doctor says so. A long enough
// default is not flagged, and other runtimes' missing timeouts never are.
func TestDiagnoseCodexMissingToolTimeout(t *testing.T) {
	d := chdir(t)
	bin := filepath.Join(d, "agent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	os.MkdirAll(filepath.Join(d, ".codex"), 0o755)
	os.WriteFile(filepath.Join(d, ".codex", "config.toml"), []byte("[mcp_servers.agent]\ncommand = \""+bin+"\"\nargs = [\"serve-mcp\"]\n"), 0o600)
	writers := append(fileWriters(runtimecfg.Codex), fileWriters(runtimecfg.ClaudeCode)...)
	cc, _ := writers[1].PlanAdd(runtimecfg.ScopeProject, "agent", runtimecfg.ServerEntry{Command: bin, Args: []string{"serve-mcp"}})
	cc.Apply()

	diagnose := func(toolTimeout string) string {
		deps := doctorDeps{
			describe: func(string, string) (*agentManifest, error) {
				return &agentManifest{Name: "agent", ToolTimeout: toolTimeout}, nil
			},
			writers: writers,
		}
		return textOf(diagnoseAgent(registry.Entry{Name: "agent", Path: bin, Scope: "local"}, deps))
	}
	if s := diagnose("2m"); !strings.Contains(s, "codex has no tool_timeout_sec (Codex default 1m0s)") || !strings.Contains(s, "2m10s") {
		t.Errorf("want a missing-timeout warning for Codex:\n%s", s)
	}
	if s := diagnose("30s"); strings.Contains(s, "tool_timeout_sec") {
		t.Errorf("a 40s need fits Codex's 60s default:\n%s", s)
	}
	if s := diagnose("2m"); strings.Contains(s, "claude-code has no") || strings.Contains(s, "claude-code timeout") {
		t.Errorf("no default is assumed for Claude Code:\n%s", s)
	}
}
