package prompt

import (
	"embed"
	"os"
	"path/filepath"
	"testing"
)

//go:embed testdata/system.md
var testFS embed.FS

func TestLoader_Load_Embedded(t *testing.T) {
	loader := NewLoader("test-agent", testFS, "testdata/system.md")

	got, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	want := "You are a test agent."
	if got != want {
		t.Errorf("Load() = %q, want %q", got, want)
	}
}

func TestLoader_IsOverridden_NoFile(t *testing.T) {
	loader := NewLoader("nonexistent-agent-xyz", testFS, "testdata/system.md")
	if loader.IsOverridden() {
		t.Error("IsOverridden() = true for nonexistent override")
	}
}

func TestLoader_Load_Override(t *testing.T) {
	// Create a temporary override directory
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	agentName := "test-override-agent"
	overrideDir := filepath.Join(tmpHome, ".agentfile", agentName)
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(overrideDir, "override.md")
	if err := os.WriteFile(overridePath, []byte("Override prompt content"), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(agentName, testFS, "testdata/system.md")

	if !loader.IsOverridden() {
		t.Fatal("IsOverridden() = false, want true")
	}

	got, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	want := "Override prompt content"
	if got != want {
		t.Errorf("Load() = %q, want %q", got, want)
	}
}

func TestLoader_OverridePath(t *testing.T) {
	loader := NewLoader("my-agent", testFS, "testdata/system.md")
	path := loader.OverridePath()
	if path == "" {
		t.Fatal("OverridePath() returned empty string")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("OverridePath() = %q, want absolute path", path)
	}
	if filepath.Base(path) != "override.md" {
		t.Errorf("OverridePath() base = %q, want override.md", filepath.Base(path))
	}
}
