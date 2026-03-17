package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teabranch/abbyfile/pkg/definition"
	"github.com/teabranch/abbyfile/pkg/fsutil"
)

// GenerateConfig configures plugin directory generation.
type GenerateConfig struct {
	OutputDir  string // parent directory (e.g., "build")
	BinaryPath string // path to the compiled binary
}

// SkillFile holds the loaded content for a single skill.
type SkillFile struct {
	Name        string
	Description string
	Content     string // markdown body
}

// pluginJSON is the .claude-plugin/plugin.json schema.
type pluginJSON struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Abbyfile    bool   `json:"abbyfile"`
}

// mcpJSON is the .mcp.json schema inside the plugin dir.
type mcpJSON struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Generate creates a Claude Code plugin directory for the given agent.
func Generate(def *definition.AgentDef, skills []SkillFile, cfg GenerateConfig) error {
	pluginDir := filepath.Join(cfg.OutputDir, def.Name+".claude-plugin")

	// Create directory structure.
	if err := os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755); err != nil {
		return fmt.Errorf("creating plugin dir: %w", err)
	}

	// 1. Write .claude-plugin/plugin.json.
	pj := pluginJSON{
		Name:        def.Name,
		Version:     def.Version,
		Description: def.Description,
		Abbyfile:    true,
	}
	if err := writeJSON(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), pj); err != nil {
		return fmt.Errorf("writing plugin.json: %w", err)
	}

	// 2. Copy binary into plugin dir.
	binaryDest := filepath.Join(pluginDir, def.Name)
	if err := fsutil.CopyFile(cfg.BinaryPath, binaryDest); err != nil {
		return fmt.Errorf("copying binary: %w", err)
	}
	if err := os.Chmod(binaryDest, 0o755); err != nil {
		return fmt.Errorf("setting binary permissions: %w", err)
	}

	// 3. Write .mcp.json pointing to the local binary.
	mj := mcpJSON{
		MCPServers: map[string]mcpServerEntry{
			def.Name: {
				Command: "./" + def.Name,
				Args:    []string{"serve-mcp"},
			},
		},
	}
	if err := writeJSON(filepath.Join(pluginDir, ".mcp.json"), mj); err != nil {
		return fmt.Errorf("writing .mcp.json: %w", err)
	}

	// 4. Write skill files.
	for _, s := range skills {
		skillDir := filepath.Join(pluginDir, "skills", s.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("creating skill dir %s: %w", s.Name, err)
		}

		skillContent := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n",
			s.Name, s.Description, s.Content)

		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
			return fmt.Errorf("writing SKILL.md for %s: %w", s.Name, err)
		}
	}

	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
