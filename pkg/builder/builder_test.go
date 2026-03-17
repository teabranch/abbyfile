package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

func TestGenerateSource(t *testing.T) {
	dir := t.TempDir()

	def := &definition.AgentDef{
		Name:        "test-agent",
		Version:     "1.0.0",
		Description: "A test agent",
		Tools:       []string{"Read", "Write"},
		Memory:      true,
		PromptBody:  "You are a test agent.\n\nDo good things.",
	}

	if err := GenerateSource(dir, def, "/fake/module/dir"); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}

	// Check main.go was generated.
	mainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	mainStr := string(mainGo)

	if !strings.Contains(mainStr, `"Read"`) {
		t.Error("main.go missing Read tool")
	}
	if !strings.Contains(mainStr, `"Write"`) {
		t.Error("main.go missing Write tool")
	}
	if !strings.Contains(mainStr, `WithName("test-agent")`) {
		t.Error("main.go missing agent name")
	}
	if !strings.Contains(mainStr, `WithVersion("1.0.0")`) {
		t.Error("main.go missing version")
	}
	if !strings.Contains(mainStr, `WithMemory(true)`) {
		t.Error("main.go missing memory")
	}

	// Check embed.go was generated.
	embedGo, err := os.ReadFile(filepath.Join(dir, "embed.go"))
	if err != nil {
		t.Fatalf("reading embed.go: %v", err)
	}
	if !strings.Contains(string(embedGo), "//go:embed prompts/system.md") {
		t.Error("embed.go missing embed directive")
	}

	// Check go.mod was generated.
	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	goModStr := string(goMod)
	if !strings.Contains(goModStr, "abbyfile-gen/test-agent") {
		t.Error("go.mod missing module name")
	}
	if !strings.Contains(goModStr, "replace") {
		t.Error("go.mod missing replace directive")
	}
	if !strings.Contains(goModStr, "/fake/module/dir") {
		t.Error("go.mod missing local module path")
	}

	// Check prompt was written.
	prompt, err := os.ReadFile(filepath.Join(dir, "prompts", "system.md"))
	if err != nil {
		t.Fatalf("reading prompt: %v", err)
	}
	if string(prompt) != "You are a test agent.\n\nDo good things." {
		t.Errorf("prompt = %q", string(prompt))
	}
}

func TestGenerateSource_NoReplace(t *testing.T) {
	dir := t.TempDir()

	def := &definition.AgentDef{
		Name:       "minimal",
		Version:    "0.1.0",
		Tools:      []string{"Read"},
		PromptBody: "Minimal.",
	}

	if err := GenerateSource(dir, def, ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}

	goMod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if strings.Contains(string(goMod), "replace") {
		t.Error("go.mod should not have replace directive when moduleDir is empty")
	}
}

func TestGenerateSource_NoMemory(t *testing.T) {
	dir := t.TempDir()

	def := &definition.AgentDef{
		Name:       "no-mem",
		Version:    "0.1.0",
		Tools:      []string{"Read"},
		Memory:     false,
		PromptBody: "No memory.",
	}

	if err := GenerateSource(dir, def, ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}

	mainGo, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(mainGo), "WithMemory") {
		t.Error("main.go should not have WithMemory when memory is disabled")
	}
}

func TestGenerateSource_CustomTools(t *testing.T) {
	dir := t.TempDir()

	def := &definition.AgentDef{
		Name:       "custom-agent",
		Version:    "1.0.0",
		Tools:      []string{"Read"},
		PromptBody: "Agent with custom tools.",
		CustomTools: []definition.CustomToolDef{
			{
				Name:        "deploy",
				Command:     "./scripts/deploy.sh",
				Description: "Deploy the application",
				Args:        []string{"--verbose"},
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"environment": map[string]any{
							"type":        "string",
							"description": "Target environment",
						},
					},
					"required": []any{"environment"},
				},
			},
			{
				Name:        "healthcheck",
				Command:     "curl",
				Description: "Check service health",
			},
		},
	}

	if err := GenerateSource(dir, def, ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}

	mainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	mainStr := string(mainGo)

	// Should have custom tool names.
	if !strings.Contains(mainStr, `"deploy"`) {
		t.Error("main.go missing deploy tool name")
	}
	if !strings.Contains(mainStr, `"healthcheck"`) {
		t.Error("main.go missing healthcheck tool name")
	}

	// Should have custom tool commands.
	if !strings.Contains(mainStr, `"./scripts/deploy.sh"`) {
		t.Error("main.go missing deploy command")
	}
	if !strings.Contains(mainStr, `"curl"`) {
		t.Error("main.go missing curl command")
	}

	// Should import encoding/json and tools package for custom tools.
	if !strings.Contains(mainStr, `"encoding/json"`) {
		t.Error("main.go missing encoding/json import")
	}
	if !strings.Contains(mainStr, `"github.com/teabranch/abbyfile/pkg/tools"`) {
		t.Error("main.go missing tools package import")
	}

	// Deploy tool should have StdinInput (has input_schema).
	if !strings.Contains(mainStr, "StdinInput: true") {
		t.Error("main.go missing StdinInput for deploy tool")
	}

	// Deploy should have args.
	if !strings.Contains(mainStr, `"--verbose"`) {
		t.Error("main.go missing --verbose arg")
	}

	// Healthcheck (no schema) should get default CLI args schema.
	// Check that there's a fallback schema with "args" property.
	// The deploy tool has json.Unmarshal, healthcheck should not.
	if strings.Count(mainStr, "json.Unmarshal") != 1 {
		t.Errorf("expected exactly 1 json.Unmarshal call (deploy only), got %d", strings.Count(mainStr, "json.Unmarshal"))
	}
}

func TestGenerateSource_NoCustomTools(t *testing.T) {
	dir := t.TempDir()

	def := &definition.AgentDef{
		Name:       "no-custom",
		Version:    "1.0.0",
		Tools:      []string{"Read"},
		PromptBody: "No custom tools.",
	}

	if err := GenerateSource(dir, def, ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}

	mainGo, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	mainStr := string(mainGo)

	// Should NOT import encoding/json or tools when no custom tools.
	if strings.Contains(mainStr, `"encoding/json"`) {
		t.Error("main.go should not import encoding/json without custom tools")
	}
	if strings.Contains(mainStr, `"github.com/teabranch/abbyfile/pkg/tools"`) {
		t.Error("main.go should not import tools package without custom tools")
	}
}

func TestDetectModuleDir(t *testing.T) {
	// Running inside the abbyfile repo should detect it.
	dir := DetectModuleDir()
	if dir == "" {
		t.Skip("not running inside abbyfile repo")
	}
	// Verify it contains go.mod with the right module.
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(data), "module github.com/teabranch/abbyfile") {
		t.Error("detected dir doesn't have the expected module")
	}
}
