// Package subagent emits a Claude Code sub-agent markdown file so a packaged
// agent can run in its own context window and return a bounded summary.
package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teabranch/abbyfile/pkg/definition"
	"gopkg.in/yaml.v3"
)

// GenerateConfig configures sub-agent emission.
type GenerateConfig struct {
	OutputDir string // parent dir; file goes to <OutputDir>/.claude/agents/<name>.md
	Model     string // optional model hint
	// MCPTools are the agent binary's own MCP tool names (custom and memory
	// tools) to grant the sub-agent alongside its native tools. Built-in
	// tools are excluded by the caller, since the native ones are listed.
	MCPTools []string
}

// mcpToolPrefix is how Claude Code names a tool served by the project-scope
// MCP server keyed <agent>. The emitted file sits beside the project
// .mcp.json written by abby build, not inside a plugin dir, so the
// plugin-scoped mcp__plugin_<plugin>_<server>__ form does not apply.
func mcpToolPrefix(agent string) string {
	return "mcp__" + agent + "__"
}

// toolsLine joins the native tools and the agent's MCP tools. An empty
// native list returns "" (omit tools:), so the sub-agent inherits every
// tool, MCP included; listing only the MCP names would strip the native ones.
func toolsLine(def *definition.AgentDef, mcpTools []string) string {
	if len(def.Tools) == 0 {
		return ""
	}
	names := append([]string{}, def.Tools...)
	for _, t := range mcpTools {
		names = append(names, mcpToolPrefix(def.Name)+t)
	}
	return strings.Join(names, ", ")
}

// frontmatter is the YAML frontmatter schema for an emitted sub-agent file.
// Field order matches struct field order: name, description, tools, model.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Tools       string `yaml:"tools,omitempty"`
	Model       string `yaml:"model,omitempty"`
}

// summaryLines returns the return-protocol summary cap.
func summaryLines(def *definition.AgentDef) int {
	if def.ContextBudget != nil && def.ContextBudget.SummaryLines > 0 {
		return def.ContextBudget.SummaryLines
	}
	return 25
}

// Generate writes .claude/agents/<name>.md and returns the file path.
func Generate(def *definition.AgentDef, cfg GenerateConfig) (string, error) {
	agentsDir := filepath.Join(cfg.OutputDir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating agents dir: %w", err)
	}

	fm := frontmatter{
		Name:        def.Name,
		Description: def.Description,
		Tools:       toolsLine(def, cfg.MCPTools),
		Model:       cfg.Model,
	}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("marshaling sub-agent frontmatter: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(fmBytes) // yaml.Marshal output ends with a newline
	sb.WriteString("---\n\n")
	sb.WriteString(def.PromptBody)
	sb.WriteString("\n\n## Return Protocol\n")
	sb.WriteString(returnProtocol(def))

	path := filepath.Join(agentsDir, def.Name+".md")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing sub-agent file: %w", err)
	}
	return path, nil
}

// returnProtocol renders the instructions for the sub-agent's final message.
func returnProtocol(def *definition.AgentDef) string {
	where := "a file path"
	if def.Memory {
		where = "a file path, or a memory key the caller can fetch with memory_read"
	}
	return fmt.Sprintf(
		"You run in an isolated context window. When you finish, return ONLY:\n"+
			"1. A ≤%d-line summary of what you did and the outcome.\n"+
			"2. Concrete artifacts the caller needs (file paths, IDs, final values).\n"+
			"Do NOT paste raw tool output, file dumps, or logs into your final message —\n"+
			"they stay in your context, not the caller's. If the caller needs full detail,\n"+
			"reference where it lives (%s) instead of inlining it.\n",
		summaryLines(def), where)
}
