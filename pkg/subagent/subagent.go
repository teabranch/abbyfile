// Package subagent emits a Claude Code sub-agent markdown file so a packaged
// agent can run in its own context window and return a bounded summary.
package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teabranch/abbyfile/pkg/definition"
)

// GenerateConfig configures sub-agent emission.
type GenerateConfig struct {
	OutputDir string // parent dir; file goes to <OutputDir>/.claude/agents/<name>.md
	Model     string // optional model hint
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

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + def.Name + "\n")
	if def.Description != "" {
		sb.WriteString("description: " + def.Description + "\n")
	}
	if len(def.Tools) > 0 {
		sb.WriteString("tools: " + strings.Join(def.Tools, ", ") + "\n")
	}
	if cfg.Model != "" {
		sb.WriteString("model: " + cfg.Model + "\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString(def.PromptBody)
	sb.WriteString("\n\n## Return Protocol\n")
	sb.WriteString(fmt.Sprintf(
		"You run in an isolated context window. When you finish, return ONLY:\n"+
			"1. A ≤%d-line summary of what you did and the outcome.\n"+
			"2. Concrete artifacts the caller needs (file paths, IDs, final values).\n"+
			"Do NOT paste raw tool output, file dumps, or logs into your final message —\n"+
			"they stay in your context, not the caller's. If the caller needs full detail,\n"+
			"reference where it lives (a path or memory:// URI) instead of inlining it.\n",
		summaryLines(def)))

	path := filepath.Join(agentsDir, def.Name+".md")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing sub-agent file: %w", err)
	}
	return path, nil
}
