package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func chdir(t *testing.T) string {
	t.Helper()
	d, _ := filepath.EvalSymlinks(t.TempDir())
	old, _ := os.Getwd()
	os.Chdir(d)
	t.Cleanup(func() { os.Chdir(old) })
	return d
}

func fileWriters(rs ...runtimecfg.Runtime) []runtimecfg.ConfigWriter {
	opts := runtimecfg.Options{Method: runtimecfg.MethodFile, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	var ws []runtimecfg.ConfigWriter
	for _, r := range rs {
		ws = append(ws, runtimecfg.For(r, opts))
	}
	return ws
}

func TestRuntimeTimeout(t *testing.T) {
	if runtimeTimeout(nil) != 0 {
		t.Error("nil manifest → 0 (omit)")
	}
	m := &agentManifest{ToolTimeout: "45s"}
	if got := runtimeTimeout(m); got != 55*time.Second {
		t.Errorf("tool only = %s", got)
	}
	m.Tools = []manifestTool{{Name: "run_command"}}
	m.Sandbox = &manifestSandbox{MaxCommandTimeout: "2m0s"}
	if got := runtimeTimeout(m); got != 130*time.Second {
		t.Errorf("with run_command = %s", got)
	}
	if got := runtimeTimeout(&agentManifest{}); got != 40*time.Second {
		t.Errorf("default = %s", got)
	}
}

func TestParseEnvFlags(t *testing.T) {
	env, err := parseEnvFlags([]string{"A=1", "B=x=y"})
	if err != nil || env["A"] != "1" || env["B"] != "x=y" {
		t.Fatalf("%v %v", env, err)
	}
	for _, bad := range []string{"noequals", "=v", "1A=v", "A B=v"} {
		if _, err := parseEnvFlags([]string{bad}); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	d := chdir(t)
	var out bytes.Buffer
	opts := installOptions{DryRun: true, Writers: fileWriters(runtimecfg.ClaudeCode, runtimecfg.Gemini), Out: &out, Err: &out}
	applied, err := applyEntries(opts, runtimecfg.ScopeProject, map[string]runtimecfg.ServerEntry{"a": {Command: "/x", Args: []string{"serve-mcp"}}})
	if err != nil || len(applied) != 2 {
		t.Fatalf("%v %v", applied, err)
	}
	for _, p := range []string{".mcp.json", ".gemini/settings.json"} {
		if _, err := os.Stat(filepath.Join(d, p)); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s", p)
		}
	}
	if !strings.Contains(out.String(), "+ ") {
		t.Errorf("dry-run must print the planned diff:\n%s", out.String())
	}
}

// Review Focus #1.
func TestInstallKeepsExistingEnv(t *testing.T) {
	d := chdir(t)
	os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(`{"mcpServers":{"a":{"type":"stdio","command":"/old","args":[],"env":{"TOKEN":"keep"}}}}`), 0o600)
	var out bytes.Buffer
	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Out: &out, Err: &out}
	if _, err := applyEntries(opts, runtimecfg.ScopeProject, map[string]runtimecfg.ServerEntry{"a": {Command: "/new", Args: []string{"serve-mcp"}}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, ".mcp.json"))
	if !strings.Contains(string(b), `"TOKEN": "keep"`) || !strings.Contains(string(b), `"/new"`) {
		t.Errorf("reinstall without --env must keep env:\n%s", b)
	}
}

func TestProjectEnvWarning(t *testing.T) {
	chdir(t)
	var out, errb bytes.Buffer
	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Env: map[string]string{"K": "v"}, Out: &out, Err: &errb}
	applyEntries(opts, runtimecfg.ScopeProject, map[string]runtimecfg.ServerEntry{"a": {Command: "/x", Env: map[string]string{"K": "v"}}})
	if !strings.Contains(errb.String(), "committed") || !strings.Contains(errb.String(), "${") {
		t.Errorf("warning = %q", errb.String())
	}
}

func TestPrintSummary(t *testing.T) {
	var out bytes.Buffer
	printSummary(&out, []appliedChange{
		{Change: runtimecfg.Change{Runtime: runtimecfg.ClaudeCode, Scope: runtimecfg.ScopeProject, Method: runtimecfg.MethodCLI, Target: ".mcp.json", Server: "a"}},
		{Change: runtimecfg.Change{Runtime: runtimecfg.Codex, Scope: runtimecfg.ScopeProject, Method: runtimecfg.MethodFile, Target: ".codex/config.toml", Server: "a", Notes: []string{"Codex loads project .codex/config.toml only in trusted projects"}}, Backup: ".codex/config.toml.abbyfile.bak"},
	}, false)
	s := out.String()
	for _, want := range []string{"RUNTIME", "SCOPE", "METHOD", "TARGET", "BACKUP", "claude-code", "cli", "codex", "file", ".abbyfile.bak", "trusted"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
}

// TestCommitReappliesFileNoopWhenEntryDisappears: a MethodFile Change planned
// as Noop must still re-check on Apply (runtimecfg's contract, see
// Change.Apply's doc comment and TestApplyRunsFileMethodEvenWhenNoop in
// pkg/runtimecfg) — if the entry vanished between plan and apply (e.g. a
// concurrent edit), commit must still write it, not skip silently.
func TestCommitReappliesFileNoopWhenEntryDisappears(t *testing.T) {
	d := chdir(t)
	os.WriteFile(filepath.Join(d, ".mcp.json"), []byte(`{"mcpServers":{"a":{"type":"stdio","command":"/x","args":["serve-mcp"]}}}`), 0o600)
	w := fileWriters(runtimecfg.ClaudeCode)[0]
	c, err := w.PlanAdd(runtimecfg.ScopeProject, "a", runtimecfg.ServerEntry{Command: "/x", Args: []string{"serve-mcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Noop {
		t.Fatal("expected a Noop plan for an identical re-add")
	}
	if err := os.Remove(filepath.Join(d, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts := installOptions{Out: &out, Err: &out}
	if _, err := commit(opts, c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d, ".mcp.json"))
	if err != nil {
		t.Fatalf("commit did not re-write the file removed after planning: %v", err)
	}
	if !strings.Contains(string(b), `"/x"`) {
		t.Errorf("re-written file missing the entry:\n%s", b)
	}
}

func TestProjectRootFor(t *testing.T) {
	if got := projectRootFor(registry.Entry{Scope: "local", Path: "/p/proj/.abbyfile/bin/a"}); got != "/p/proj" {
		t.Errorf("local = %q", got)
	}
	if got := projectRootFor(registry.Entry{Scope: "global", Path: "/usr/local/bin/a"}); got != "" {
		t.Errorf("global = %q", got)
	}
	if got := projectRootFor(registry.Entry{Scope: "local", Path: "/odd/place/a"}); got != "" {
		t.Errorf("unknown layout = %q", got)
	}
}

func TestConfigOptionsFromEnv(t *testing.T) {
	t.Setenv("ABBY_CONFIG_METHOD", "file")
	o, err := configOptions("")
	if err != nil || o.Method != runtimecfg.MethodFile {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := configOptions("bogus"); err == nil {
		t.Error("bogus must fail")
	}
}
