package agent

import (
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
