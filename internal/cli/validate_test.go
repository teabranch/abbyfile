package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

func TestValidateCommand_AllPass(t *testing.T) {
	loader := prompt.NewLoader("test-agent", testFS, "testdata/system.md")
	reg := tools.NewRegistry()
	reg.Register(tools.CLI("date", "date", "Get the date"))
	reg.Register(tools.BuiltinTool("echo", "Echo input", nil, func(input map[string]any) (string, error) {
		return "ok", nil
	}))

	cmd := NewValidateCommand("test-agent", "1.0.0", loader, reg, false)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[PASS] Prompt:") {
		t.Errorf("missing prompt pass check in output:\n%s", out)
	}
	if !strings.Contains(out, "[PASS] Tool \"date\"") {
		t.Errorf("missing date tool pass check in output:\n%s", out)
	}
	if !strings.Contains(out, "[PASS] Tool \"echo\"") {
		t.Errorf("missing echo tool pass check in output:\n%s", out)
	}
	if !strings.Contains(out, "[PASS] Version: 1.0.0") {
		t.Errorf("missing version pass check in output:\n%s", out)
	}
	if !strings.Contains(out, "Validation PASSED") {
		t.Errorf("missing PASSED summary in output:\n%s", out)
	}
}

func TestValidateCommand_MissingCommand(t *testing.T) {
	loader := prompt.NewLoader("test-agent", testFS, "testdata/system.md")
	reg := tools.NewRegistry()
	reg.Register(tools.CLI("bad", "nonexistent-command-xyz", "Won't be found"))

	cmd := NewValidateCommand("test-agent", "1.0.0", loader, reg, false)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation to fail")
	}

	out := buf.String()
	if !strings.Contains(out, "[FAIL] Tool \"bad\"") {
		t.Errorf("missing tool fail check in output:\n%s", out)
	}
	if !strings.Contains(out, "Validation FAILED") {
		t.Errorf("missing FAILED summary in output:\n%s", out)
	}
}

func TestValidateCommand_BuiltinNoHandler(t *testing.T) {
	loader := prompt.NewLoader("test-agent", testFS, "testdata/system.md")
	reg := tools.NewRegistry()
	reg.Register(&tools.Definition{
		Name:    "broken",
		Builtin: true,
		Handler: nil,
	})

	cmd := NewValidateCommand("test-agent", "1.0.0", loader, reg, false)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validation to fail")
	}

	out := buf.String()
	if !strings.Contains(out, "[FAIL] Tool \"broken\"") {
		t.Errorf("missing broken tool fail in output:\n%s", out)
	}
}

func TestValidateCommand_MemoryEnabled(t *testing.T) {
	loader := prompt.NewLoader("test-agent", testFS, "testdata/system.md")
	reg := tools.NewRegistry()

	cmd := NewValidateCommand("test-validate-mem", "1.0.0", loader, reg, true)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[PASS] Memory:") {
		t.Errorf("missing memory pass check in output:\n%s", out)
	}
}

func TestValidateCommand_MemoryDisabled(t *testing.T) {
	loader := prompt.NewLoader("test-agent", testFS, "testdata/system.md")
	reg := tools.NewRegistry()

	cmd := NewValidateCommand("test-agent", "1.0.0", loader, reg, false)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[INFO] Memory: disabled") {
		t.Errorf("missing memory disabled info in output:\n%s", out)
	}
}
