package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/builtins"
	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// newTestAgent builds an agent with a name, a version and the package's
// embedded prompt FS (see testFS in agent_test.go). WithName in opts
// overrides the base name, because options apply in order.
func newTestAgent(t *testing.T, opts ...Option) *Agent {
	t.Helper()
	base := []Option{WithName("sb-agent"), WithVersion("0.0.1"), WithPromptFS(testFS, "testdata/system.md")}
	a, err := New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestApplyConfigOverrides_Sandbox(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(cfgPath, []byte("sandbox:\n  bash: unrestricted\n  allow_commands: [\"ls\"]\n  max_command_timeout: 9s\n  allowed_dirs: [\"/tmp\"]\n"), 0o600)
	a := newTestAgent(t, WithConfigPath(cfgPath), WithSandbox(sandbox.Config{AllowCommands: []string{"go test *"}}))
	if a.sandbox.Bash != sandbox.BashUnrestricted || a.sandbox.AllowCommands[0] != "ls" ||
		a.sandbox.MaxCommandTimeout != 9*time.Second || a.sandbox.AllowedDirs[0] != "/tmp" {
		t.Fatalf("sandbox after overrides = %+v", a.sandbox)
	}
}

func TestDefaultSandboxWhenNotSet(t *testing.T) {
	a := newTestAgent(t, WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")))
	if a.sandbox.Normalize().Bash != sandbox.BashRestricted || len(a.sandbox.AllowCommands) != 0 {
		t.Fatalf("default sandbox = %+v", a.sandbox)
	}
}

func TestSandboxedToolDefs_RewritesRunCommandDescription(t *testing.T) {
	a := newTestAgent(t, WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")),
		WithTools(builtins.RunCommandTool()),
		WithSandbox(sandbox.Config{AllowCommands: []string{"go test *"}}))
	sb, err := a.buildSandbox()
	if err != nil {
		t.Fatal(err)
	}
	defs := a.sandboxedToolDefs(sb)
	if !strings.Contains(defs[0].Description, "`go test *`") {
		t.Errorf("description = %q", defs[0].Description)
	}
	if strings.Contains(a.toolDefs[0].Description, "go test") {
		t.Error("original definition must not be mutated")
	}
}

// Review Focus #4.
func TestAgentSandboxAllowsSpillRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := newTestAgent(t, WithName("spiller"), WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")))
	sb, err := a.buildSandbox()
	if err != nil {
		t.Fatal(err)
	}
	spill := filepath.Join(tools.SpillDir("spiller"), "run_command-abc.txt")
	if _, err := sb.Resolve(spill, sandbox.Read); err != nil {
		t.Fatalf("spill file must be readable: %v", err)
	}
	if _, err := sb.Resolve(spill, sandbox.Write); err == nil {
		t.Fatal("spill dir must be read-only")
	}
}

// Fix round 1, issue 1c: an invalid sandbox override (e.g. a hand-edited
// config.yaml with sandbox.allowed_dirs: [""], predating the config-set
// validation added for issue 1b) must not lock every invocation out.
// effectiveSandbox falls back to the default sandbox instead.
func TestEffectiveSandbox_FallsBackOnInvalidOverride(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("sandbox:\n  allowed_dirs: [\"\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(cfgPath))
	var stderr bytes.Buffer
	a.stderr = &stderr

	// buildSandbox (no fallback) must fail on the invalid override.
	if _, err := a.buildSandbox(); err == nil {
		t.Fatal("buildSandbox should fail on an invalid sandbox.allowed_dirs override")
	}

	sb, err := a.effectiveSandbox()
	if err != nil {
		t.Fatalf("effectiveSandbox should fall back, not error: %v", err)
	}
	if !strings.Contains(stderr.String(), "falling back to the compiled sandbox") {
		t.Errorf("stderr = %q, want a message about falling back to the compiled sandbox", stderr.String())
	}
	want := sandbox.Default().Normalize()
	got := sb.Config()
	if got.Bash != want.Bash || len(got.AllowCommands) != 0 || got.MaxCommandTimeout != want.MaxCommandTimeout {
		t.Fatalf("fallback sandbox = %+v, want default %+v", got, want)
	}
	if len(sb.AllowedDirs()) == 0 {
		t.Fatal("fallback sandbox must still confine to the working directory")
	}
}

// Fix round 2: a generated agent's compiled-in sandbox (WithSandbox) can be
// narrower than sandbox.Default() (e.g. AllowedDirs: ["data"]). If a
// config.yaml override makes the *effective* sandbox invalid,
// effectiveSandbox must fall back to that compiled sandbox — not silently
// widen access to sandbox.Default()'s AllowedDirs: ["."].
func TestEffectiveSandbox_FallsBackToCompiledNotDefault(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	// A hand-edited config.yaml with an invalid bash value. It does not
	// touch allowed_dirs, so the compiled AllowedDirs: ["data"] would
	// otherwise still be in a.sandbox — but the whole effective sandbox is
	// invalid because of bash, so buildSandbox() must fail wholesale.
	if err := os.WriteFile(cfgPath, []byte("sandbox:\n  bash: yolo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(cfgPath), WithSandbox(sandbox.Config{AllowedDirs: []string{"data"}}))
	var stderr bytes.Buffer
	a.stderr = &stderr

	if _, err := a.buildSandbox(); err == nil {
		t.Fatal("buildSandbox should fail on an invalid sandbox.bash override")
	}

	sb, err := a.effectiveSandbox()
	if err != nil {
		t.Fatalf("effectiveSandbox should fall back to the compiled sandbox, not error: %v", err)
	}
	if !strings.Contains(stderr.String(), "falling back to the compiled sandbox") {
		t.Errorf("stderr = %q, want a message about falling back to the compiled sandbox", stderr.String())
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	resolvedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(resolvedCwd, "data")

	got := sb.AllowedDirs()
	if len(got) != 1 || got[0] != wantDir {
		t.Fatalf("fallback AllowedDirs = %v, want [%s] (the compiled sandbox, not the working directory)", got, wantDir)
	}
	if got[0] == resolvedCwd {
		t.Fatal("fallback must not widen to the working directory (sandbox.Default())")
	}
}

// Finding 1: when the compiled-in sandbox is itself unbuildable (here, an
// AllowedDirs entry that is a dangling symlink), tier 3 must be a deny-all
// sandbox, not sandbox.Default() (which would widen access to the process
// working directory). effectiveSandbox must still return err=nil so the
// CLI stays usable.
func TestEffectiveSandbox_TierThreeIsDenyAll(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Symlink(filepath.Join(tmp, "missing-target"), filepath.Join(tmp, "data")); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")),
		WithSandbox(sandbox.Config{AllowedDirs: []string{filepath.Join(tmp, "data")}}))
	var stderr bytes.Buffer
	a.stderr = &stderr

	if _, err := a.buildSandbox(); err == nil {
		t.Fatal("buildSandbox should fail: allowed_dirs entry is a dangling symlink")
	}

	sb, err := a.effectiveSandbox()
	if err != nil {
		t.Fatalf("effectiveSandbox must return err=nil even when the compiled sandbox also fails: %v", err)
	}
	if len(sb.AllowedDirs()) != 0 {
		t.Fatalf("deny-all fallback must have no allowed dirs, got %v", sb.AllowedDirs())
	}
	if !strings.Contains(stderr.String(), "file and command tools are disabled") {
		t.Errorf("stderr = %q, want a message about tools being disabled", stderr.String())
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Resolve(filepath.Join(cwd, "x"), sandbox.Read); err == nil {
		t.Fatal("deny-all fallback must deny every path")
	}
	if _, err := sb.CheckCommand("echo hi"); err == nil {
		t.Fatal("deny-all fallback must refuse every command")
	}
}

// Finding 6: with no config.yaml sandbox override, the effective sandbox IS
// the compiled sandbox, so retrying it at tier 2 would fail identically.
// effectiveSandbox must skip that redundant retry, print exactly one
// Error: line, and not suggest "config reset sandbox" (there is no
// override to reset).
func TestEffectiveSandbox_NoOverride_SkipsRedundantRetry(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Symlink(filepath.Join(tmp, "missing-target"), filepath.Join(tmp, "data")); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(filepath.Join(t.TempDir(), "none.yaml")),
		WithSandbox(sandbox.Config{AllowedDirs: []string{filepath.Join(tmp, "data")}}))
	var stderr bytes.Buffer
	a.stderr = &stderr

	if _, err := a.effectiveSandbox(); err != nil {
		t.Fatalf("effectiveSandbox must return err=nil: %v", err)
	}
	out := stderr.String()
	if strings.Contains(out, "config reset sandbox") {
		t.Errorf("no config.yaml override exists; stderr must not suggest resetting one: %q", out)
	}
	if strings.Contains(out, "falling back to the compiled sandbox") {
		t.Errorf("no override to fall back from; stderr must not claim a fallback retry: %q", out)
	}
	if n := strings.Count(out, "Error:"); n != 1 {
		t.Errorf("expected exactly one Error: line, got %d: %q", n, out)
	}
}

// Finding 6: with a config.yaml sandbox override present, the reset hint
// must still appear (and only one Error: line, since the compiled sandbox
// is valid here).
func TestEffectiveSandbox_WithOverride_PrintsResetHint(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("sandbox:\n  bash: yolo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(cfgPath))
	var stderr bytes.Buffer
	a.stderr = &stderr

	if _, err := a.effectiveSandbox(); err != nil {
		t.Fatalf("effectiveSandbox should fall back, not error: %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "config reset sandbox") {
		t.Errorf("a config.yaml override exists; stderr should hint at resetting it: %q", out)
	}
	if n := strings.Count(out, "Error:"); n != 1 {
		t.Errorf("compiled sandbox is valid; expected exactly one Error: line, got %d: %q", n, out)
	}
}

// Finding 6: a sandbox: block with no fields set (sandbox: {}) must not
// count as an override — applyConfigOverrides tracks that a sandbox field
// was actually applied, not merely that the YAML key was present with an
// empty map that parses to a non-nil *SandboxOverride with all-nil fields.
func TestApplyConfigOverrides_EmptySandboxBlockIsNotAnOverride(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("sandbox: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(cfgPath))
	if a.sandboxOverridden {
		t.Fatal("an empty sandbox: {} block must not count as an override")
	}
}

// Finding 4: each sandbox override field, applied individually.
func TestApplyConfigOverrides_Sandbox_PerField(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want func(t *testing.T, a *Agent)
	}{
		{
			"allowed_dirs",
			"sandbox:\n  allowed_dirs: [\"/tmp\"]\n",
			func(t *testing.T, a *Agent) {
				if len(a.sandbox.AllowedDirs) != 1 || a.sandbox.AllowedDirs[0] != "/tmp" {
					t.Errorf("AllowedDirs = %v", a.sandbox.AllowedDirs)
				}
			},
		},
		{
			"bash",
			"sandbox:\n  bash: unrestricted\n",
			func(t *testing.T, a *Agent) {
				if a.sandbox.Bash != sandbox.BashUnrestricted {
					t.Errorf("Bash = %v", a.sandbox.Bash)
				}
			},
		},
		{
			"allow_commands",
			"sandbox:\n  allow_commands: [\"go test *\"]\n",
			func(t *testing.T, a *Agent) {
				if len(a.sandbox.AllowCommands) != 1 || a.sandbox.AllowCommands[0] != "go test *" {
					t.Errorf("AllowCommands = %v", a.sandbox.AllowCommands)
				}
			},
		},
		{
			"max_command_timeout",
			"sandbox:\n  max_command_timeout: 9s\n",
			func(t *testing.T, a *Agent) {
				if a.sandbox.MaxCommandTimeout != 9*time.Second {
					t.Errorf("MaxCommandTimeout = %v", a.sandbox.MaxCommandTimeout)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			a := newTestAgent(t, WithConfigPath(cfgPath))
			if !a.sandboxOverridden {
				t.Error("a sandbox: override was applied; sandboxOverridden must be true")
			}
			tt.want(t, a)
		})
	}
}

// Finding 4: an invalid max_command_timeout keeps the compiled value
// (already true for the other three fields' malformed-input paths, which
// simply don't set the field; max_command_timeout parses eagerly so this
// locks in the same behavior for it).
func TestApplyConfigOverrides_InvalidMaxCommandTimeoutKeepsCompiled(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("sandbox:\n  max_command_timeout: not-a-duration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(t, WithConfigPath(cfgPath), WithSandbox(sandbox.Config{MaxCommandTimeout: 42 * time.Second}))
	if a.sandbox.MaxCommandTimeout != 42*time.Second {
		t.Errorf("MaxCommandTimeout = %v, want compiled value 42s kept", a.sandbox.MaxCommandTimeout)
	}
}
