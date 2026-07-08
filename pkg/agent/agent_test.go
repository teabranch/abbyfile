package agent

import (
	"embed"
	"path/filepath"
	"testing"

	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/tools"
)

//go:embed testdata/system.md
var testFS embed.FS

func TestNew_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{
			name:    "missing name",
			opts:    []Option{WithVersion("1.0.0"), WithPromptFS(testFS, "testdata/system.md")},
			wantErr: "agent name is required",
		},
		{
			name:    "missing version",
			opts:    []Option{WithName("test"), WithPromptFS(testFS, "testdata/system.md")},
			wantErr: "agent version is required",
		},
		{
			name:    "missing prompt",
			opts:    []Option{WithName("test"), WithVersion("1.0.0")},
			wantErr: "prompt filesystem is required",
		},
		{
			name: "valid",
			opts: []Option{WithName("test"), WithVersion("1.0.0"), WithPromptFS(testFS, "testdata/system.md")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opts...)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}
		})
	}
}

func TestAgent_ToolRegistration(t *testing.T) {
	a, err := New(
		WithName("test-agent"),
		WithVersion("1.0.0"),
		WithPromptFS(testFS, "testdata/system.md"),
		WithTools(
			tools.CLI("echo_tool", "echo", "Echo text back"),
			tools.CLI("date_tool", "date", "Get current date"),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(a.toolDefs) != 2 {
		t.Errorf("toolDefs count = %d, want 2", len(a.toolDefs))
	}
}

func TestAgent_MemoryOption(t *testing.T) {
	a, err := New(
		WithName("test-agent"),
		WithVersion("1.0.0"),
		WithPromptFS(testFS, "testdata/system.md"),
		WithMemory(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !a.memoryEnabled {
		t.Error("memoryEnabled = false, want true")
	}
}

func TestAgent_Defaults(t *testing.T) {
	a, err := New(
		WithName("test-agent"),
		WithVersion("1.0.0"),
		WithPromptFS(testFS, "testdata/system.md"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if a.toolTimeout == 0 {
		t.Error("toolTimeout should have a default value")
	}
	if a.memoryEnabled {
		t.Error("memoryEnabled should default to false")
	}
}

func TestApplyConfigOverrides_ContextBudget(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	lines := 42
	over := "spill"
	if err := config.WriteTo(cfgPath, &config.Config{
		ContextBudget: &config.ContextBudgetOverride{
			MaxOutputLines: &lines,
			OnOverflow:     &over,
		},
	}); err != nil {
		t.Fatal(err)
	}

	a, err := New(
		WithName("t"), WithVersion("0.0.1"),
		WithPromptFS(testFS, "testdata/system.md"),
		WithConfigPath(cfgPath),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.budget.MaxOutputLines != 42 {
		t.Fatalf("MaxOutputLines = %d, want 42", a.budget.MaxOutputLines)
	}
	if a.budget.OnOverflow != tools.OverflowSpill {
		t.Fatalf("OnOverflow = %v, want spill", a.budget.OnOverflow)
	}
	// Untouched fields keep the shipped default.
	if a.budget.HeadLines != 100 {
		t.Fatalf("HeadLines = %d, want default 100", a.budget.HeadLines)
	}
}
