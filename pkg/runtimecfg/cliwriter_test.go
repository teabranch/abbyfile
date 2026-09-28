package runtimecfg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	dir  string // working directory the fake command "ran" in
	name string
	args []string
}

// fakeRunner records calls; fail maps "subcommand" (args[1]) to an error.
func fakeRunner(calls *[]call, fail map[string]error) CommandRunner {
	return func(_ context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
		*calls = append(*calls, call{dir, name, append([]string(nil), args...)})
		if len(args) > 1 {
			if err, ok := fail[args[1]]; ok {
				return nil, []byte(err.Error()), err
			}
		}
		return nil, nil, nil
	}
}

func cliOpts(calls *[]call, fail map[string]error, onPath ...string) Options {
	return Options{Method: MethodAuto, Run: fakeRunner(calls, fail), LookPath: func(n string) (string, error) {
		for _, p := range onPath {
			if p == n {
				return "/fake/" + n, nil
			}
		}
		return "", os.ErrNotExist
	}}
}

func TestClaudeUsesCLIWhenOnPath(t *testing.T) {
	chdirTemp(t)
	var calls []call
	w := For(ClaudeCode, cliOpts(&calls, nil, "claude"))
	c, err := w.PlanAdd(ScopeProject, "agent", entry())
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("change = %+v, %v", c, err)
	}
	if !strings.Contains(c.Preview, "claude mcp add-json -s project agent") {
		t.Errorf("preview must show the command: %s", c.Preview)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].args[0] != "mcp" || calls[0].args[1] != "add-json" {
		t.Fatalf("calls = %+v (no remove when the entry is absent)", calls)
	}
	js := calls[0].args[len(calls[0].args)-1]
	for _, want := range []string{`"type":"stdio"`, `"command":"/abs/bin/agent"`, `"timeout":130000`} {
		if !strings.Contains(js, want) {
			t.Errorf("add-json payload missing %s: %s", want, js)
		}
	}
}

func TestClaudeReplacesExistingViaRemoveThenAdd(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"type":"stdio","command":"/old","args":[],"env":{"K":"v"}}}}`), 0o600)
	var calls []call
	c, _ := For(ClaudeCode, cliOpts(&calls, nil, "claude")).PlanAdd(ScopeProject, "agent", entry())
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].args[1] != "remove" || calls[1].args[1] != "add-json" {
		t.Fatalf("calls = %+v", calls)
	}
	if !strings.Contains(calls[1].args[len(calls[1].args)-1], `"K":"v"`) {
		t.Error("existing env must be carried into the new entry")
	}
}

// Review Focus #4.
func TestClaudeCLIRestoresOnAddFailure(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"type":"stdio","command":"/old","args":[]}}}`), 0o600)
	var calls []call
	n := 0
	opts := cliOpts(&calls, nil, "claude")
	opts.Run = func(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, call{dir, name, args})
		if args[1] == "add-json" {
			n++
			if n == 1 {
				return nil, []byte("boom"), errors.New("exit status 1")
			}
		}
		return nil, nil, nil
	}
	c, _ := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry())
	_, err := c.Apply()
	if err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("err = %v, want failure that reports the restore", err)
	}
	last := calls[len(calls)-1]
	if last.args[1] != "add-json" || !strings.Contains(last.args[len(last.args)-1], `"/old"`) {
		t.Errorf("last call must re-add the old entry: %+v", last)
	}
}

func TestClaudeFallsBackToFileWhenUnownedKeysExist(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"command":"/old","args":[],"alwaysAllow":["x"]}}}`), 0o600)
	var calls []call
	c, _ := For(ClaudeCode, cliOpts(&calls, nil, "claude")).PlanAdd(ScopeProject, "agent", entry())
	if c.Method != MethodFile {
		t.Fatalf("method = %s; add-json would drop alwaysAllow", c.Method)
	}
}

func TestClaudeRemoveViaCLI(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"command":"/old","args":[]}}}`), 0o600)
	var calls []call
	c, err := For(ClaudeCode, cliOpts(&calls, nil, "claude")).PlanRemove(ScopeProject, "agent")
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("%+v %v", c, err)
	}
	c.Apply()
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "mcp remove -s project agent" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestGeminiCLIOnlyForUserScopeWithoutCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chdirTemp(t)
	var calls []call
	w := For(Gemini, cliOpts(&calls, nil, "gemini"))
	proj, _ := w.PlanAdd(ScopeProject, "agent", entry())
	if proj.Method != MethodFile {
		t.Errorf("project scope needs cwd → file, got %s", proj.Method)
	}
	e := entry()
	e.Cwd = ""
	e.Env = map[string]string{"A": "1"}
	user, _ := w.PlanAdd(ScopeUser, "agent", e)
	if user.Method != MethodCLI {
		t.Fatalf("user scope without cwd → cli, got %s", user.Method)
	}
	user.Apply()
	got := strings.Join(calls[0].args, " ")
	if got != "mcp add -s user -e A=1 --timeout 130000 agent /abs/bin/agent serve-mcp" {
		t.Errorf("gemini args = %q", got)
	}
}

func TestCodexNeverUsesCLI(t *testing.T) {
	chdirTemp(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	var calls []call
	c, _ := For(Codex, cliOpts(&calls, nil, "codex")).PlanAdd(ScopeUser, "agent", entry())
	if c.Method != MethodFile {
		t.Errorf("codex method = %s", c.Method)
	}
	forced := cliOpts(&calls, nil, "codex")
	forced.Method = MethodCLI
	if _, err := For(Codex, forced).PlanAdd(ScopeUser, "agent", entry()); err == nil || !strings.Contains(err.Error(), "codex") {
		t.Errorf("--config-method cli for codex must explain why it can't: %v", err)
	}
}

// cliAddChange must set Noop like cliRemoveChange already does; otherwise a
// reinstall of an unchanged entry always runs remove+add-json against the
// real CLI, and Task 6 would report "Updated" for a no-op.
func TestClaudeCLIAddIsNoopWhenUnchanged(t *testing.T) {
	chdirTemp(t)
	w := For(ClaudeCode, fileOpts)
	c1, err := w.PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Apply(); err != nil {
		t.Fatal(err)
	}

	var calls []call
	wc := For(ClaudeCode, cliOpts(&calls, nil, "claude"))
	c2, err := wc.PlanAdd(ScopeProject, "agent", entry())
	if err != nil {
		t.Fatal(err)
	}
	if c2.Method != MethodCLI {
		t.Fatalf("method = %s, want cli", c2.Method)
	}
	if !c2.Noop {
		t.Fatal("re-adding an identical entry via CLI must be a no-op")
	}
	if _, err := c2.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("Apply on a noop CLI change must not invoke the CLI: %+v", calls)
	}
}

// The Gemini CLI's "mcp add" overwrites the whole entry, so it must not be
// chosen when it would silently drop a key abby is not currently setting
// but that the existing entry already has (cwd, timeout).
func TestGeminiCLIWouldDropExistingCwdFallsBackToFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chdirTemp(t)
	path := filepath.Join(home, ".gemini", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"mcpServers":{"agent":{"command":"/old","args":[],"cwd":"/abs/proj"}}}`), 0o600)
	var calls []call
	e := entry()
	e.Cwd = ""
	c, err := For(Gemini, cliOpts(&calls, nil, "gemini")).PlanAdd(ScopeUser, "agent", e)
	if err != nil {
		t.Fatal(err)
	}
	if c.Method != MethodFile {
		t.Fatalf("method = %s; gemini's CLI would silently erase the existing cwd", c.Method)
	}
}

func TestGeminiCLIWouldDropExistingTimeoutFallsBackToFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chdirTemp(t)
	path := filepath.Join(home, ".gemini", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"mcpServers":{"agent":{"command":"/old","args":[],"timeout":5000}}}`), 0o600)
	var calls []call
	e := entry()
	e.Cwd = ""
	e.Timeout = 0
	c, err := For(Gemini, cliOpts(&calls, nil, "gemini")).PlanAdd(ScopeUser, "agent", e)
	if err != nil {
		t.Fatal(err)
	}
	if c.Method != MethodFile {
		t.Fatalf("method = %s; gemini's CLI would silently erase the existing timeout", c.Method)
	}
}

func TestForcedCLIWithoutBinaryErrors(t *testing.T) {
	chdirTemp(t)
	var calls []call
	opts := cliOpts(&calls, nil)
	opts.Method = MethodCLI
	if _, err := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry()); err == nil || !strings.Contains(err.Error(), "not on PATH") {
		t.Errorf("err = %v", err)
	}
}

// Fix round 1, item 1: claude's own "mcp" subcommands resolve project scope
// from THEIR process's cwd, not from any flag, so abby must run them in
// Options.ProjectRoot — which Task 6 sets independently of the process's
// actual working directory (e.g. uninstall/update/doctor run from
// elsewhere) — or the CLI edits the wrong project's .mcp.json.
func TestClaudeCLIProjectScopeRunsInProjectRootNotProcessCwd(t *testing.T) {
	chdirTemp(t)                                  // process cwd = A, deliberately unrelated to the project root
	root, _ := filepath.EvalSymlinks(t.TempDir()) // Options.ProjectRoot = B
	os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"agent":{"command":"/old","args":[]}}}`), 0o600)

	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	opts.ProjectRoot = root
	w := For(ClaudeCode, opts)

	c, err := w.PlanAdd(ScopeProject, "agent", entry())
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("change = %+v, %v", c, err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	for _, cl := range calls {
		if cl.dir != root {
			t.Errorf("add call %+v ran in dir %q, want Options.ProjectRoot %q", cl, cl.dir, root)
		}
	}

	calls = nil
	rm, err := w.PlanRemove(ScopeProject, "agent")
	if err != nil || rm.Method != MethodCLI {
		t.Fatalf("remove = %+v, %v", rm, err)
	}
	if _, err := rm.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].dir != root {
		t.Fatalf("remove calls = %+v, want dir %q", calls, root)
	}
}

func TestClaudeCLIUserScopeRunsInProcessCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	chdirTemp(t)
	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	opts.ProjectRoot = "/should/not/be/used/for/user/scope"
	c, err := For(ClaudeCode, opts).PlanAdd(ScopeUser, "agent", entry())
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("change = %+v, %v", c, err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].dir != "" {
		t.Fatalf("calls = %+v, want an empty dir (process cwd) for user scope", calls)
	}
}

// Fix round 1, item 2: claude's "mcp remove" exits 1 with a "No MCP server
// named" message when the name isn't registered; abby's desired end state
// (no such entry) already holds, so that must not surface as an abby error.
func TestClaudeRemoveViaCLITreatsAlreadyAbsentAsSuccess(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"command":"/old","args":[]}}}`), 0o600)
	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	opts.Run = func(_ context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, call{dir, name, args})
		return nil, []byte(`No MCP server named "agent" in project config`), errors.New("exit status 1")
	}
	c, err := For(ClaudeCode, opts).PlanRemove(ScopeProject, "agent")
	if err != nil || c.Method != MethodCLI {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := c.Apply(); err != nil {
		t.Fatalf("a remove failing only because the entry is already absent must not be an error: %v", err)
	}
}

// Same tolerance for the first ("remove") step of Claude's replace flow: it
// must not trigger the restore-on-failure path (nothing was removed) and
// must let the add-json step still run.
func TestClaudeReplaceTreatsAlreadyAbsentRemoveAsSuccessAndProceeds(t *testing.T) {
	d := chdirTemp(t)
	os.WriteFile(d+"/.mcp.json", []byte(`{"mcpServers":{"agent":{"type":"stdio","command":"/old","args":[]}}}`), 0o600)
	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	opts.Run = func(_ context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, call{dir, name, args})
		if args[1] == "remove" {
			return nil, []byte(`No MCP server named "agent" in project config`), errors.New("exit status 1")
		}
		return nil, nil, nil
	}
	c, _ := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry())
	if _, err := c.Apply(); err != nil {
		t.Fatalf("an already-absent remove must not block the add or trigger a restore: %v", err)
	}
	if len(calls) != 2 || calls[0].args[1] != "remove" || calls[1].args[1] != "add-json" {
		t.Fatalf("calls = %+v", calls)
	}
}

// Fix round 1, item 3.
func TestClaudeCLITimeoutIsReported(t *testing.T) {
	chdirTemp(t)
	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	opts.Run = func(_ context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
		return nil, nil, context.DeadlineExceeded
	}
	c, _ := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry())
	_, err := c.Apply()
	if err == nil || !strings.Contains(err.Error(), "timed out after 30s") {
		t.Fatalf("err = %v, want a message naming the 30s timeout", err)
	}
}

func TestClaudeCLIErrorFallsBackToStdoutWhenStderrEmpty(t *testing.T) {
	chdirTemp(t)
	var calls []call
	opts := cliOpts(&calls, nil, "claude")
	opts.Run = func(_ context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
		return []byte(" some diagnostic on stdout \n"), nil, errors.New("exit status 1")
	}
	c, _ := For(ClaudeCode, opts).PlanAdd(ScopeProject, "agent", entry())
	_, err := c.Apply()
	if err == nil || !strings.Contains(err.Error(), "some diagnostic on stdout") {
		t.Fatalf("err = %v, want it to include stdout when stderr is empty", err)
	}
}
