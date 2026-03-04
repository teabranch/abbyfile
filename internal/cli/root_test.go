package cli

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

//go:embed testdata/system.md
var testFS embed.FS

func newTestRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.CLI("date", "date", "Get current date"))
	return reg
}

func newTestOpts() Options {
	return Options{
		Name:        "test-agent",
		Version:     "1.0.0",
		Description: "A test agent",
		Loader:      prompt.NewLoader("test-agent", testFS, "testdata/system.md"),
		Registry:    newTestRegistry(),
		Memory:      true,
	}
}

func TestRootCommand_Version(t *testing.T) {
	cmd := NewRootCommand(newTestOpts())
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	got := buf.String()
	want := "test-agent v1.0.0\n"
	if got != want {
		t.Errorf("version output = %q, want %q", got, want)
	}
}

func TestRootCommand_CustomInstructions(t *testing.T) {
	cmd := NewRootCommand(newTestOpts())
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--custom-instructions"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	got := strings.TrimSpace(buf.String())
	want := "You are a test agent."
	if got != want {
		t.Errorf("custom-instructions output = %q, want %q", got, want)
	}
}

func TestRootCommand_Describe(t *testing.T) {
	cmd := NewRootCommand(newTestOpts())
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--describe"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var manifest AgentManifest
	if err := json.Unmarshal(buf.Bytes(), &manifest); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if manifest.Name != "test-agent" {
		t.Errorf("Name = %q, want %q", manifest.Name, "test-agent")
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", manifest.Version, "1.0.0")
	}
	if !manifest.Memory {
		t.Error("Memory = false, want true")
	}
	if len(manifest.Tools) != 1 {
		t.Errorf("Tools count = %d, want 1", len(manifest.Tools))
	}
	if manifest.Tools[0].Name != "date" {
		t.Errorf("Tool name = %q, want %q", manifest.Tools[0].Name, "date")
	}
}

func TestRootCommand_NoArgs_ShowsHelp(t *testing.T) {
	cmd := NewRootCommand(newTestOpts())
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	got := buf.String()
	if got == "" {
		t.Error("expected help output, got empty string")
	}
}
