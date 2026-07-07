package mcp

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/prompt"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// Internal (white-box) tests for handshakeInstructions, which is unexported
// and therefore must live in package mcp rather than mcp_test. See
// bridge_test.go for the black-box test suite.

//go:embed testdata/system.md
var internalTestPromptFS embed.FS

// testLoaderWithPrompt returns a prompt.Loader that loads exactly the given
// content. It uses the Loader's override-file mechanism: a temp HOME is set
// up with an override.md containing the desired prompt text, so the
// embedded fallback (internalTestPromptFS/testdata/system.md) is never read.
func testLoaderWithPrompt(t *testing.T, content string) *prompt.Loader {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	agentName := "test-agent-" + strings.ReplaceAll(t.Name(), "/", "-")
	overrideDir := filepath.Join(tmpHome, ".abbyfile", agentName)
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("creating override dir: %v", err)
	}
	overridePath := filepath.Join(overrideDir, "override.md")
	if err := os.WriteFile(overridePath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing override file: %v", err)
	}

	return prompt.NewLoader(agentName, internalTestPromptFS, "testdata/system.md")
}

func TestBridge_NonEagerInstructions_ReturnsStub(t *testing.T) {
	b := NewBridge(BridgeConfig{
		Name:              "t",
		Version:           "0.0.1",
		Description:       "A test agent",
		Loader:            testLoaderWithPrompt(t, "FULL SYSTEM PROMPT BODY THAT IS LONG"),
		Registry:          tools.NewRegistry(),
		EagerInstructions: false,
	})
	got := b.handshakeInstructions()
	if strings.Contains(got, "FULL SYSTEM PROMPT BODY") {
		t.Fatalf("non-eager handshake leaked full prompt: %q", got)
	}
	if !strings.Contains(got, "get_instructions") {
		t.Fatalf("stub should mention get_instructions: %q", got)
	}
}

func TestBridge_EagerInstructions_ReturnsFullPrompt(t *testing.T) {
	b := NewBridge(BridgeConfig{
		Name:              "t",
		Version:           "0.0.1",
		Loader:            testLoaderWithPrompt(t, "FULL SYSTEM PROMPT BODY THAT IS LONG"),
		Registry:          tools.NewRegistry(),
		EagerInstructions: true,
	})
	got := b.handshakeInstructions()
	if !strings.Contains(got, "FULL SYSTEM PROMPT BODY") {
		t.Fatalf("eager handshake missing full prompt: %q", got)
	}
}
