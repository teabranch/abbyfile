package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func runCmd(t *testing.T, cfg sandbox.Config, input map[string]any) (string, error) {
	t.Helper()
	return handleRunCommand(sandboxCtx(t, realTempDir(t), cfg), input)
}

func TestRunCommand_Allowed(t *testing.T) {
	out, err := runCmd(t, sandbox.Config{AllowCommands: []string{"echo *"}}, map[string]any{"command": "echo hello"})
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

// Review Focus #2.
func TestRunCommand_EmptyAllowlistRefused(t *testing.T) {
	_, err := runCmd(t, sandbox.Config{}, map[string]any{"command": "echo hello"})
	if err == nil || !strings.Contains(err.Error(), "allow_commands is empty") || !strings.Contains(err.Error(), "config set sandbox.allow_commands") {
		t.Fatalf("err = %v, want an actionable refusal", err)
	}
}

func TestRunCommand_NotAllowed(t *testing.T) {
	_, err := runCmd(t, sandbox.Config{AllowCommands: []string{"echo *"}}, map[string]any{"command": "ls"})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCommand_NoShell(t *testing.T) {
	root := realTempDir(t)
	ctx := sandboxCtx(t, root, sandbox.Config{AllowCommands: []string{"echo *"}})
	if _, err := handleRunCommand(ctx, map[string]any{"command": "echo hi > " + filepath.Join(root, "f")}); err == nil || !strings.Contains(err.Error(), "no shell") {
		t.Fatalf("redirect err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "f")); !os.IsNotExist(err) {
		t.Fatal("redirect target must not be created")
	}
	out, err := handleRunCommand(ctx, map[string]any{"command": "echo $HOME"})
	if err != nil || strings.TrimSpace(out) != "$HOME" {
		t.Fatalf("no expansion expected, got %q, %v", out, err)
	}
}

func TestRunCommand_Unrestricted(t *testing.T) {
	out, err := runCmd(t, sandbox.Config{Bash: sandbox.BashUnrestricted}, map[string]any{"command": "echo a | tr a b"})
	if err != nil || strings.TrimSpace(out) != "b" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunCommand_TimeoutClamped(t *testing.T) {
	start := time.Now()
	_, err := runCmd(t, sandbox.Config{AllowCommands: []string{"sleep *"}, MaxCommandTimeout: 200 * time.Millisecond},
		map[string]any{"command": "sleep 30", "timeout": float64(30)})
	if err == nil || !strings.Contains(err.Error(), "timed out after 200ms") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("clamp not applied")
	}
}

func TestCommandTimeout(t *testing.T) {
	max := 120 * time.Second
	tests := []struct {
		in   map[string]any
		max  time.Duration
		want time.Duration
	}{
		{map[string]any{}, max, 30 * time.Second},
		{map[string]any{"timeout": float64(10)}, max, 10 * time.Second},
		{map[string]any{"timeout": float64(500)}, max, max},
		{map[string]any{"timeout": float64(0)}, max, 30 * time.Second},
		{map[string]any{"timeout": float64(-1)}, max, 30 * time.Second},
		{map[string]any{}, 5 * time.Second, 5 * time.Second},
		// Finding 5: a huge requested timeout must saturate at maxTimeout
		// instead of overflowing time.Duration(t*1e9) (platform-dependent
		// on a float64->int64 conversion that doesn't fit).
		{map[string]any{"timeout": float64(1e30)}, max, max},
		// Finding 5: a sub-microsecond requested timeout truncates to a
		// zero or negative Duration when converted; it floors to 1ms
		// rather than becoming an instantly-expired command.
		{map[string]any{"timeout": float64(1e-15)}, max, time.Millisecond},
	}
	for _, tt := range tests {
		if got := commandTimeout(tt.in, tt.max); got != tt.want {
			t.Errorf("commandTimeout(%v, %s) = %s, want %s", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestRunCommand_BoundedOutput(t *testing.T) {
	ctx := tools.WithOutputLimit(sandboxCtx(t, realTempDir(t), sandbox.Config{AllowCommands: []string{"head *"}}), 100)
	out, err := handleRunCommand(ctx, map[string]any{"command": "head -c 5000 /dev/zero"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "[output truncated at 100 bytes]") || len(out) > 140 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestRunCommandDescription(t *testing.T) {
	if d := RunCommandDescription(sandbox.Config{Bash: sandbox.BashUnrestricted}); !strings.Contains(d, "sh -c") {
		t.Errorf("unrestricted: %q", d)
	}
	if d := RunCommandDescription(sandbox.Default()); !strings.Contains(d, "every call is refused") {
		t.Errorf("empty: %q", d)
	}
	d := RunCommandDescription(sandbox.Config{AllowCommands: []string{"go test *", "git status"}})
	if !strings.Contains(d, "`go test *`") || !strings.Contains(d, "`git status`") || !strings.Contains(d, "without a shell") {
		t.Errorf("restricted: %q", d)
	}
}

func TestRunCommandTool_Definition(t *testing.T) {
	def := RunCommandTool()
	if def.Name != RunCommandToolName || !def.UsesCommandTimeout || def.HandlerCtx == nil {
		t.Fatalf("def = %+v", def)
	}
	if strings.Contains(def.Description, "sh -c") {
		t.Errorf("default description must describe restricted mode: %q", def.Description)
	}
}
