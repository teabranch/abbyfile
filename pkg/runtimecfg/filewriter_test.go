package runtimecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fileOpts forces the file method with no CLIs on PATH.
var fileOpts = Options{Method: MethodFile, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}

func chdirTemp(t *testing.T) string {
	t.Helper()
	d, _ := filepath.EvalSymlinks(t.TempDir())
	old, _ := os.Getwd()
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return d
}

func copyFixture(t *testing.T, name, dst string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

var fixtureDir = func() string { d, _ := filepath.Abs("testdata"); return d }()

func entry() ServerEntry {
	return ServerEntry{Command: "/abs/bin/agent", Args: []string{"serve-mcp"}, Cwd: "/abs/proj", Timeout: 130 * time.Second}
}

func TestClaudeUserScopeFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path := filepath.Join(home, ".claude.json")
	copyFixture(t, "claude.json", path)
	orig, _ := os.ReadFile(path)

	w := For(ClaudeCode, fileOpts)
	if p, _ := w.ConfigPath(ScopeUser); p != path {
		t.Fatalf("ConfigPath(user) = %q, want %q (D7)", p, path)
	}
	c, err := w.PlanAdd(ScopeUser, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if c.Method != MethodFile || c.Target != path || !strings.Contains(c.Preview, "+") {
		t.Fatalf("change = %+v", c)
	}
	if b, _ := os.ReadFile(path); string(b) != string(orig) {
		t.Fatal("PlanAdd must not write")
	}
	backup, err := c.Apply()
	if err != nil || backup != path+".abbyfile.bak" {
		t.Fatalf("Apply = %q, %v", backup, err)
	}
	var got map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	var want map[string]json.RawMessage
	json.Unmarshal(orig, &want)
	for k := range want {
		if k == "mcpServers" {
			continue
		}
		if compactJSON(got[k]) != compactJSON(want[k]) {
			t.Errorf("key %q changed: %s -> %s", k, want[k], got[k])
		}
	}
	e, ok, _ := w.Lookup(ScopeUser, "agent")
	if !ok || e.Command != "/abs/bin/agent" || e.Timeout != 130*time.Second || e.Cwd != "" {
		t.Errorf("Lookup = %+v %v (Claude has no cwd; timeout in ms)", e, ok)
	}
	if _, ok, _ := w.Lookup(ScopeUser, "existing"); !ok {
		t.Error("existing server lost")
	}
}

func TestClaudeConfigDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if p, _ := For(ClaudeCode, fileOpts).ConfigPath(ScopeUser); p != filepath.Join(dir, ".claude.json") {
		t.Errorf("ConfigPath = %q", p)
	}
}

func TestProjectRootIsExplicit(t *testing.T) {
	chdirTemp(t) // process cwd is somewhere else
	root, _ := filepath.EvalSymlinks(t.TempDir())
	opts := fileOpts
	opts.ProjectRoot = root
	c, err := For(Codex, opts).PlanAdd(ScopeProject, "agent", entry())
	if err != nil || c.Target != filepath.Join(root, ".codex", "config.toml") {
		t.Fatalf("target = %q, %v; project scope must follow Options.ProjectRoot, not the process cwd", c.Target, err)
	}
}

func TestClaudeProjectScopeInCwd(t *testing.T) {
	d := chdirTemp(t)
	c, err := For(ClaudeCode, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type": "stdio"`, `"command": "/abs/bin/agent"`, `"timeout": 130000`} {
		if !strings.Contains(string(b), want) {
			t.Errorf(".mcp.json missing %s:\n%s", want, b)
		}
	}
	if fi, _ := os.Stat(filepath.Join(d, ".mcp.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %v", fi.Mode().Perm())
	}
}

func TestGeminiProjectKeepsTrustAndSetsCwd(t *testing.T) {
	d := chdirTemp(t)
	copyFixture(t, "gemini.json", filepath.Join(d, ".gemini", "settings.json"))
	w := For(Gemini, fileOpts)
	c, err := w.PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, ".gemini", "settings.json"))
	for _, want := range []string{`"trust": true`, `"includeTools"`, `"cwd": "/abs/proj"`, `"timeout": 130000`, `"selectedAuthType"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings.json missing %s:\n%s", want, b)
		}
	}
}

func TestCodexUserScopeHonoursCodexHome(t *testing.T) {
	ch := t.TempDir()
	t.Setenv("CODEX_HOME", ch)
	copyFixture(t, "codex.toml", filepath.Join(ch, "config.toml"))
	w := For(Codex, fileOpts)
	c, err := w.PlanAdd(ScopeUser, "agent", ServerEntry{Command: "/n", Args: []string{"serve-mcp"}, Timeout: 125500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if c.Target != filepath.Join(ch, "config.toml") {
		t.Errorf("target = %q", c.Target)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(ch, "config.toml"))
	for _, want := range []string{"tool_timeout_sec = 126", "startup_timeout_sec = 30", "enabled = false", "# keep me"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config.toml missing %q:\n%s", want, b)
		}
	}
}

func TestCodexProjectNotesTrust(t *testing.T) {
	chdirTemp(t)
	c, err := For(Codex, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Notes) == 0 || !strings.Contains(c.Notes[0], "trusted") {
		t.Errorf("notes = %v", c.Notes)
	}
}

// Review Focus #3.
func TestFileWriterRefusesUnparsable(t *testing.T) {
	d := chdirTemp(t)
	p := filepath.Join(d, ".gemini", "settings.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	commented := "{\n  // my settings\n  \"theme\": \"x\"\n}\n"
	os.WriteFile(p, []byte(commented), 0o644)
	_, err := For(Gemini, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v, want refusal naming %s", err, p)
	}
	if b, _ := os.ReadFile(p); string(b) != commented {
		t.Error("file modified")
	}
	if _, err := os.Stat(p + ".abbyfile.bak"); !os.IsNotExist(err) {
		t.Error("no backup may be created on refusal")
	}
}

func TestPlanRemove(t *testing.T) {
	d := chdirTemp(t)
	w := For(ClaudeCode, fileOpts)
	c, _ := w.PlanAdd(ScopeProject, "agent", entry())
	c.Apply()
	rm, err := w.PlanRemove(ScopeProject, "agent")
	if err != nil || rm.Noop || !rm.Remove {
		t.Fatalf("PlanRemove = %+v, %v", rm, err)
	}
	if _, err := rm.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := w.Lookup(ScopeProject, "agent"); ok {
		t.Error("still present")
	}
	again, _ := w.PlanRemove(ScopeProject, "agent")
	if !again.Noop {
		t.Error("second remove must be a no-op")
	}
	_ = d
}

func TestApplyReplansOnceOnConcurrentWrite(t *testing.T) {
	d := chdirTemp(t)
	p := filepath.Join(d, ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers":{}}`), 0o600)
	c, _ := For(ClaudeCode, fileOpts).PlanAdd(ScopeProject, "agent", entry())
	os.WriteFile(p, []byte(`{"mcpServers":{"someone":{"command":"x"}}}`), 0o600)
	if _, err := c.Apply(); err != nil {
		t.Fatalf("one concurrent change must be absorbed by re-planning: %v", err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "someone") || !strings.Contains(string(b), "agent") {
		t.Errorf("re-plan lost data: %s", b)
	}
}

func compactJSON(r json.RawMessage) string {
	var v any
	json.Unmarshal(r, &v)
	b, _ := json.Marshal(v)
	return string(b)
}
