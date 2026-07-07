// Package builder generates Go source code from agent definitions
// and compiles them into standalone binaries.
package builder

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/teabranch/abbyfile/pkg/definition"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// BuildConfig controls the build process.
type BuildConfig struct {
	OutputDir     string // directory for compiled binaries
	ModuleVersion string // published module version (e.g. "v0.8.0")
	ModuleDir     string // local module path for replace directive (dev/CI only)
	TargetOS      string // GOOS for cross-compilation (empty = native)
	TargetArch    string // GOARCH for cross-compilation (empty = native)
	Parallelism   int    // max concurrent builds (0 = sequential)
}

// customToolData holds pre-serialized custom tool info for code generation.
type customToolData struct {
	Name            string
	Command         string
	Description     string
	Args            []string
	InputSchemaJSON string // JSON string of InputSchema, empty if no schema
	StdinInput      bool   // true when InputSchema is present
}

// templateData is the data passed to Go code generation templates.
type templateData struct {
	Name          string
	Version       string
	Description   string
	Tools         []string
	CustomTools   []customToolData
	Memory        bool
	ModuleVersion string // published module version (e.g. "v0.8.0")
	ModuleDir     string // local module path for replace directive (dev/CI only)
	Budget        *budgetData
}

// budgetData holds pre-serialized context budget info for code generation.
type budgetData struct {
	MaxOutputLines    int
	MaxOutputBytes    int64
	OnOverflow        string
	HeadLines         int
	TailLines         int
	SummaryLines      int
	EagerInstructions bool
	PerTool           map[string]budgetData
}

// Shipped defaults, mirrored from tools.DefaultContextBudget so that a
// partial frontmatter context_budget block still yields a complete,
// valid budget in generated code.
const (
	defaultMaxOutputLines = 2000
	defaultMaxOutputBytes = 262144
	defaultHeadLines      = 100
	defaultTailLines      = 40
	defaultSummaryLines   = 25
	defaultOnOverflow     = "head-tail"
)

// buildBudgetData maps a definition.ContextBudgetDef into a budgetData,
// applying shipped defaults to zero-valued scalar fields and normalizing
// an empty OnOverflow to "head-tail" so the generated shaper never sees
// an invalid strategy.
func buildBudgetData(cb *definition.ContextBudgetDef) *budgetData {
	if cb == nil {
		return nil
	}

	bd := &budgetData{
		MaxOutputLines: cb.MaxOutputLines,
		MaxOutputBytes: cb.MaxOutputBytes,
		OnOverflow:     cb.OnOverflow,
		HeadLines:      cb.HeadLines,
		TailLines:      cb.TailLines,
		SummaryLines:   cb.SummaryLines,
	}
	if cb.EagerInstructions != nil {
		bd.EagerInstructions = *cb.EagerInstructions
	}

	if bd.MaxOutputLines == 0 {
		bd.MaxOutputLines = defaultMaxOutputLines
	}
	if bd.MaxOutputBytes == 0 {
		bd.MaxOutputBytes = defaultMaxOutputBytes
	}
	if bd.OnOverflow == "" {
		bd.OnOverflow = defaultOnOverflow
	}
	if bd.HeadLines == 0 {
		bd.HeadLines = defaultHeadLines
	}
	if bd.TailLines == 0 {
		bd.TailLines = defaultTailLines
	}
	if bd.SummaryLines == 0 {
		bd.SummaryLines = defaultSummaryLines
	}

	if len(cb.PerTool) > 0 {
		bd.PerTool = make(map[string]budgetData, len(cb.PerTool))
		for name, pt := range cb.PerTool {
			ptData := budgetData{
				MaxOutputLines: pt.MaxOutputLines,
				MaxOutputBytes: pt.MaxOutputBytes,
				OnOverflow:     pt.OnOverflow,
				HeadLines:      pt.HeadLines,
				TailLines:      pt.TailLines,
				SummaryLines:   pt.SummaryLines,
			}
			if pt.EagerInstructions != nil {
				ptData.EagerInstructions = *pt.EagerInstructions
			}
			// Per-tool scalars left at 0 mean "inherit base" via
			// effectiveFor's merge (Task 1); only the overflow
			// strategy needs a non-empty default since an empty
			// string is never a valid OverflowStrategy.
			if ptData.OnOverflow == "" {
				ptData.OnOverflow = defaultOnOverflow
			}
			bd.PerTool[name] = ptData
		}
	}

	return bd
}

// Build generates source code from an AgentDef and compiles it into a binary.
func Build(def *definition.AgentDef, cfg BuildConfig) error {
	tmpDir, err := os.MkdirTemp("", "abbyfile-build-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := GenerateSource(tmpDir, def, cfg.ModuleVersion, cfg.ModuleDir); err != nil {
		return fmt.Errorf("generating source: %w", err)
	}

	// Make output dir absolute so it works when go build runs in tmpDir.
	absOutputDir, err := filepath.Abs(cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("resolving output dir: %w", err)
	}
	if err := os.MkdirAll(absOutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// go mod tidy to resolve dependencies.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = tmpDir
	tidy.Stdout = os.Stderr
	tidy.Stderr = os.Stderr
	if err := tidy.Run(); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}

	// Compile.
	outputName := def.Name
	if cfg.TargetOS != "" && cfg.TargetArch != "" {
		outputName = fmt.Sprintf("%s-%s-%s", def.Name, cfg.TargetOS, cfg.TargetArch)
	}
	outputPath := filepath.Join(absOutputDir, outputName)
	build := exec.Command("go", "build", "-o", outputPath, ".")
	build.Dir = tmpDir
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if cfg.TargetOS != "" || cfg.TargetArch != "" {
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if cfg.TargetOS != "" {
			build.Env = append(build.Env, "GOOS="+cfg.TargetOS)
		}
		if cfg.TargetArch != "" {
			build.Env = append(build.Env, "GOARCH="+cfg.TargetArch)
		}
	}
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	return nil
}

// BuildAll builds all agent definitions, optionally in parallel.
func BuildAll(defs map[string]*definition.AgentDef, cfg BuildConfig) error {
	if cfg.Parallelism <= 1 || len(defs) <= 1 {
		// Sequential build.
		for name, def := range defs {
			fmt.Fprintf(os.Stderr, "Building %s...\n", name)
			if err := Build(def, cfg); err != nil {
				return fmt.Errorf("building %s: %w", name, err)
			}
			fmt.Fprintf(os.Stderr, "  → %s/%s\n", cfg.OutputDir, name)
		}
		return nil
	}

	// Parallel build with bounded concurrency.
	type result struct {
		name string
		err  error
	}
	sem := make(chan struct{}, cfg.Parallelism)
	results := make(chan result, len(defs))

	for name, def := range defs {
		sem <- struct{}{}
		go func(n string, d *definition.AgentDef) {
			defer func() { <-sem }()
			fmt.Fprintf(os.Stderr, "Building %s...\n", n)
			err := Build(d, cfg)
			if err == nil {
				fmt.Fprintf(os.Stderr, "  → %s/%s\n", cfg.OutputDir, n)
			}
			results <- result{n, err}
		}(name, def)
	}

	var errs []string
	for range defs {
		r := <-results
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.name, r.err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("build failures:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// GenerateSource writes generated Go files into dir from an AgentDef.
// moduleVersion is the published abbyfile module version (e.g. "v0.8.0").
// moduleDir, if non-empty, adds a replace directive for local development/CI.
func GenerateSource(dir string, def *definition.AgentDef, moduleVersion, moduleDir string) error {
	// Resolve moduleDir to absolute path so the replace directive works
	// from the temp build directory where go mod tidy runs.
	if moduleDir != "" {
		abs, err := filepath.Abs(moduleDir)
		if err != nil {
			return fmt.Errorf("resolving module dir: %w", err)
		}
		moduleDir = abs
	}

	var customTools []customToolData
	for _, ct := range def.CustomTools {
		ctd := customToolData{
			Name:        ct.Name,
			Command:     ct.Command,
			Description: ct.Description,
			Args:        ct.Args,
		}
		if ct.InputSchema != nil {
			schemaJSON, err := json.Marshal(ct.InputSchema)
			if err != nil {
				return fmt.Errorf("marshaling input_schema for tool %q: %w", ct.Name, err)
			}
			ctd.InputSchemaJSON = string(schemaJSON)
			ctd.StdinInput = true
		}
		customTools = append(customTools, ctd)
	}

	data := templateData{
		Name:          def.Name,
		Version:       def.Version,
		Description:   def.Description,
		Tools:         def.Tools,
		CustomTools:   customTools,
		Memory:        def.Memory,
		ModuleVersion: moduleVersion,
		ModuleDir:     moduleDir,
		Budget:        buildBudgetData(def.ContextBudget),
	}

	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	// Generate main.go
	if err := writeTemplate(tmpl, "main.go.tmpl", filepath.Join(dir, "main.go"), data); err != nil {
		return err
	}

	// Generate embed.go
	if err := writeTemplate(tmpl, "embed.go.tmpl", filepath.Join(dir, "embed.go"), data); err != nil {
		return err
	}

	// Generate go.mod
	if err := writeTemplate(tmpl, "go.mod.tmpl", filepath.Join(dir, "go.mod"), data); err != nil {
		return err
	}

	// Write prompt file.
	promptDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		return fmt.Errorf("creating prompt dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "system.md"), []byte(def.PromptBody), 0o644); err != nil {
		return fmt.Errorf("writing prompt: %w", err)
	}

	return nil
}

func writeTemplate(tmpl *template.Template, name, outPath string, data any) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", outPath, err)
	}
	defer f.Close()

	if err := tmpl.ExecuteTemplate(f, name, data); err != nil {
		return fmt.Errorf("executing template %s: %w", name, err)
	}
	return nil
}
