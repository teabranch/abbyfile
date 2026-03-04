package agent

import (
	"embed"
	"testing"

	"github.com/teabranch/agentfile/pkg/tools"
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
