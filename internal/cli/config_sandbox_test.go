package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/sandbox"
)

func TestSandboxFields(t *testing.T) {
	defaults := CompiledDefaults{Sandbox: sandbox.Config{AllowCommands: []string{"go test *"}}.Normalize()}
	bash := "unrestricted"
	cfg := &config.Config{Sandbox: &config.SandboxOverride{Bash: &bash}}
	got := map[string]string{}
	for _, f := range sandboxFields(cfg, defaults) {
		got[f.key] = f.value + " (" + f.source + ")"
	}
	want := map[string]string{
		"sandbox.allowed_dirs":        `["."] (compiled)`,
		"sandbox.bash":                "unrestricted (override)",
		"sandbox.allow_commands":      `["go test *"] (compiled)`,
		"sandbox.max_command_timeout": (2 * time.Minute).String() + " (compiled)",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestSandboxSetWarning(t *testing.T) {
	if w := sandboxSetWarning("sandbox.bash", "unrestricted"); !strings.Contains(w, "any shell command") {
		t.Errorf("bash warning = %q", w)
	}
	if w := sandboxSetWarning("sandbox.allowed_dirs", "/,."); !strings.Contains(w, "whole filesystem") {
		t.Errorf("dirs warning = %q", w)
	}
	if w := sandboxSetWarning("sandbox.bash", "restricted"); w != "" {
		t.Errorf("no warning expected, got %q", w)
	}
}

func TestPrintManifestIncludesSandbox(t *testing.T) {
	root := t.TempDir()
	sb, err := sandbox.New(sandbox.Config{Bash: sandbox.BashUnrestricted}, root)
	if err != nil {
		t.Fatal(err)
	}
	m := buildManifest(Options{Name: "a", Version: "1", Sandbox: sb})
	if m.Sandbox == nil || m.Sandbox.Bash != "unrestricted" || len(m.Sandbox.Warnings) == 0 || m.Sandbox.AllowCommands == nil {
		t.Fatalf("manifest sandbox = %+v", m.Sandbox)
	}
}
