package builder

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/definition"
)

func intPtr(i int) *int { return &i }

func int64Ptr(i int64) *int64 { return &i }

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

	if err := GenerateSource(dir, def, "v1.0.0", ""); err != nil {
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

	// Check go.mod was generated with published module version.
	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	goModStr := string(goMod)
	if !strings.Contains(goModStr, "abbyfile-gen/test-agent") {
		t.Error("go.mod missing module name")
	}
	if !strings.Contains(goModStr, "github.com/teabranch/abbyfile v1.0.0") {
		t.Error("go.mod missing published module version")
	}
	if strings.Contains(goModStr, "replace") {
		t.Error("go.mod should not contain replace directive")
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

func TestGenerateSource_NoMemory(t *testing.T) {
	dir := t.TempDir()

	def := &definition.AgentDef{
		Name:       "no-mem",
		Version:    "0.1.0",
		Tools:      []string{"Read"},
		Memory:     false,
		PromptBody: "No memory.",
	}

	if err := GenerateSource(dir, def, "v1.0.0", ""); err != nil {
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

	if err := GenerateSource(dir, def, "v1.0.0", ""); err != nil {
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

func TestGenerateSource_EmitsContextBudget(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "b", Version: "0.0.1", Description: "d", Tools: []string{"Read"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			MaxOutputLines: intPtr(500),
			OnOverflow:     "spill",
			HeadLines:      50,
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "agent.WithContextBudget(") {
		t.Fatalf("main.go missing WithContextBudget:\n%s", s)
	}
	if !strings.Contains(s, "MaxOutputLines: 500") {
		t.Fatalf("main.go missing MaxOutputLines value:\n%s", s)
	}
	if !strings.Contains(s, `OnOverflow: tools.OverflowStrategy("spill")`) {
		t.Fatalf("main.go missing OnOverflow:\n%s", s)
	}
}

func TestGenerateSource_NoBudget_NoWithContextBudget(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{Name: "b", Version: "0.0.1", Tools: []string{"Read"}, PromptBody: "x"}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	data, _ := os.ReadFile(src)
	if strings.Contains(string(data), "WithContextBudget") {
		t.Fatal("main.go should omit WithContextBudget when no budget declared")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v\n---\n%s", err, data)
	}
}

// TestGenerateSource_BudgetProducesParseableGo guards the main.go.tmpl
// context-budget block (including the per-tool map literal) against
// syntax breakage — e.g. an unbalanced brace or stray comma — that the
// string-matching tests above would not catch.
func TestGenerateSource_BudgetProducesParseableGo(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "p", Version: "0.0.1", Description: "d", Tools: []string{"Read"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			MaxOutputLines: intPtr(500),
			OnOverflow:     "spill",
			HeadLines:      50,
			PerTool: map[string]definition.ContextBudgetDef{
				"run_command": {OnOverflow: "head-tail"},
			},
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	src := filepath.Join(dir, "main.go")
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		// Read the file and include it so a failure is debuggable.
		data, _ := os.ReadFile(src)
		t.Fatalf("generated main.go is not valid Go: %v\n---\n%s", err, data)
	}
}

// TestGenerateSource_PerToolOnOverflow_InheritsWhenEmpty locks Bug I1's fix:
// a per_tool entry that omits on_overflow must generate an empty
// tools.OverflowStrategy("") so tools.ContextBudget.effectiveFor inherits
// the base strategy at runtime, instead of being silently forced to
// "head-tail".
func TestGenerateSource_PerToolOnOverflow_InheritsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "inherit", Version: "0.0.1", Description: "d", Tools: []string{"Read"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			OnOverflow: "spill",
			PerTool: map[string]definition.ContextBudgetDef{
				"run_command": {HeadLines: 10},
			},
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	src := filepath.Join(dir, "main.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `OnOverflow: tools.OverflowStrategy("spill")`) {
		t.Fatalf("main.go missing base OnOverflow spill:\n%s", s)
	}
	if !strings.Contains(s, `OnOverflow: tools.OverflowStrategy("")`) {
		t.Fatalf("main.go per-tool OnOverflow should be empty (inherit base), got:\n%s", s)
	}
	if strings.Count(s, `tools.OverflowStrategy("head-tail")`) != 0 {
		t.Fatalf("per-tool OnOverflow must not be defaulted to head-tail:\n%s", s)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v\n---\n%s", err, data)
	}
}

// TestGenerateSource_ZeroMaxOutputLines_MeansUnlimited locks Bug I2's fix:
// an explicit MaxOutputLines pointer to 0 in frontmatter must generate
// MaxOutputLines: 0 (unlimited, per tools.Shaper's Shape semantics), while
// a nil pointer (omitted) must fall back to the shipped default of 2000.
func TestGenerateSource_ZeroMaxOutputLines_MeansUnlimited(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "unlimited", Version: "0.0.1", Description: "d", Tools: []string{"Read"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			MaxOutputLines: intPtr(0),
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "MaxOutputLines: 0") {
		t.Fatalf("main.go with explicit 0 should emit MaxOutputLines: 0 (unlimited):\n%s", s)
	}

	// Now the omitted (nil) case must still default to 2000.
	dir2 := t.TempDir()
	def2 := &definition.AgentDef{
		Name: "defaulted", Version: "0.0.1", Description: "d", Tools: []string{"Read"},
		PromptBody:    "body",
		ContextBudget: &definition.ContextBudgetDef{OnOverflow: "spill"},
	}
	if err := GenerateSource(dir2, def2, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	data2, err := os.ReadFile(filepath.Join(dir2, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	s2 := string(data2)
	if !strings.Contains(s2, "MaxOutputLines: 2000") {
		t.Fatalf("main.go with nil MaxOutputLines should default to 2000:\n%s", s2)
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

	if err := GenerateSource(dir, def, "v1.0.0", ""); err != nil {
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

func TestGenerateSource_PerToolInlineLarge(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "p", Version: "0.0.1", Description: "d", Tools: []string{"Bash"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			PerTool: map[string]definition.ContextBudgetDef{
				"run_command": {InlineLarge: true},
			},
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "InlineLarge: true") {
		t.Fatalf("generated main.go lacks InlineLarge: true\n%s", src)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v\n%s", err, src)
	}
}

// Agents that don't opt in must not reference the field at all, so they
// still compile against an abbyfile module that predates it.
func TestGenerateSource_PerToolWithoutInlineLarge_OmitsField(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "p", Version: "0.0.1", Description: "d", Tools: []string{"Bash"},
		PromptBody: "body",
		ContextBudget: &definition.ContextBudgetDef{
			PerTool: map[string]definition.ContextBudgetDef{
				"run_command": {OnOverflow: "spill"},
			},
		},
	}
	if err := GenerateSource(dir, def, "v0.9.1", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "InlineLarge") {
		t.Fatalf("generated main.go must not mention InlineLarge when unset\n%s", src)
	}
}

func TestGenerateSource_EmitsSandbox(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "s", Version: "0.0.1", Description: "d", Tools: []string{"Read", "Bash"}, PromptBody: "b",
		Sandbox: &definition.SandboxDef{AllowCommands: []string{`go test *`, `echo "a b"`}, MaxCommandTimeout: "45s"},
	}
	if err := GenerateSource(dir, def, "v0.11.0", ""); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	data, _ := os.ReadFile(src)
	s := string(data)
	for _, want := range []string{
		`"github.com/teabranch/abbyfile/pkg/sandbox"`,
		"agent.WithSandbox(sandbox.Config{",
		`AllowedDirs: []string{"."}`,
		`Bash: sandbox.BashMode("restricted")`,
		`"go test *"`, `"echo \"a b\""`,
		"MaxCommandTimeout: 45000000000, // 45s",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("main.go missing %q:\n%s", want, s)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v\n---\n%s", err, data)
	}
}

func TestGenerateSource_NoSandbox_OmitsWithSandbox(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{Name: "s", Version: "0.0.1", Tools: []string{"Read"}, PromptBody: "b"}
	if err := GenerateSource(dir, def, "v0.11.0", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(data), "WithSandbox") || strings.Contains(string(data), "pkg/sandbox") {
		t.Fatalf("no sandbox block must emit nothing:\n%s", data)
	}
}

func TestGenerateSource_EmptyAllowCommandsIsValidGo(t *testing.T) {
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name: "s", Version: "0.0.1", Tools: []string{"Bash"}, PromptBody: "b",
		Sandbox: &definition.SandboxDef{Bash: "restricted"},
	}
	if err := GenerateSource(dir, def, "v0.11.0", ""); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	if _, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.AllErrors); err != nil {
		data, _ := os.ReadFile(src)
		t.Fatalf("invalid Go: %v\n%s", err, data)
	}
}

func TestGenerateSource_SchemaLessCustomToolSkipsJSONImport(t *testing.T) {
	// encoding/json is only used to decode an input_schema; importing it
	// without one fails `go build` with an unused import.
	dir := t.TempDir()
	def := &definition.AgentDef{
		Name:        "plain-tool",
		Version:     "1.0.0",
		PromptBody:  "x",
		CustomTools: []definition.CustomToolDef{{Name: "lint", Command: "echo", Description: "Lint"}},
	}
	if err := GenerateSource(dir, def, "v1.0.0", ""); err != nil {
		t.Fatalf("GenerateSource: %v", err)
	}
	mainGo, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(mainGo), `"encoding/json"`) {
		t.Fatalf("main.go imports encoding/json with no input_schema:\n%s", mainGo)
	}
}
