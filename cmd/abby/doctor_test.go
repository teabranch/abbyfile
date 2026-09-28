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
		describe: func(string) (*agentManifest, error) {
			return &agentManifest{Name: "agent", Version: "1.0.0", Sandbox: &manifestSandbox{Bash: "restricted", AllowCommands: []string{"go test ./..."}, AllowedDirs: []string{d}}}, nil
		},
		handshake: func(_ context.Context, _, pv string) (string, error) {
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
		describe: func(string) (*agentManifest, error) {
			return &agentManifest{Name: "agent", ToolTimeout: "30s", Sandbox: &manifestSandbox{Bash: "unrestricted", Warnings: []string{"sandbox.bash is unrestricted"}}}, nil
		},
		handshake: func(context.Context, string, string) (string, error) { return "", errors.New("connection closed") },
		writers:   []runtimecfg.ConfigWriter{w},
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
