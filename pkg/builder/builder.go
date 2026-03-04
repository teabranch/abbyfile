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

	"github.com/teabranch/agentfile/pkg/definition"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// BuildConfig controls the build process.
type BuildConfig struct {
	OutputDir  string // directory for compiled binaries
	ModuleDir  string // local agentfile module path (for replace directive)
	TargetOS   string // GOOS for cross-compilation (empty = native)
	TargetArch string // GOARCH for cross-compilation (empty = native)
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
	Name        string
	Version     string
	Description string
	Tools       []string
	CustomTools []customToolData
	Memory      bool
	Replace     string // local module path, empty if using published module
}

// Build generates source code from an AgentDef and compiles it into a binary.
func Build(def *definition.AgentDef, cfg BuildConfig) error {
	tmpDir, err := os.MkdirTemp("", "agentfile-build-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := GenerateSource(tmpDir, def, cfg.ModuleDir); err != nil {
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

// BuildAll builds all agent definitions.
func BuildAll(defs map[string]*definition.AgentDef, cfg BuildConfig) error {
	for name, def := range defs {
		fmt.Fprintf(os.Stderr, "Building %s...\n", name)
		if err := Build(def, cfg); err != nil {
			return fmt.Errorf("building %s: %w", name, err)
		}
		fmt.Fprintf(os.Stderr, "  → %s/%s\n", cfg.OutputDir, name)
	}
	return nil
}

// GenerateSource writes generated Go files into dir from an AgentDef.
func GenerateSource(dir string, def *definition.AgentDef, moduleDir string) error {
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
		Name:        def.Name,
		Version:     def.Version,
		Description: def.Description,
		Tools:       def.Tools,
		CustomTools: customTools,
		Memory:      def.Memory,
		Replace:     moduleDir,
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

// DetectModuleDir checks if we're running inside the agentfile framework repo.
// If yes, returns the repo root for use as a replace directive. Otherwise returns "".
func DetectModuleDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		modPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(modPath)
		if err == nil {
			if strings.Contains(string(data), "module github.com/teabranch/agentfile") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
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
