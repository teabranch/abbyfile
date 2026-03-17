package benchmarks

import (
	"fmt"
	"strings"
)

// ClaudeCodeBaseline estimates the token overhead Claude Code carries
// before any MCP servers are loaded. These numbers are derived from
// observable Claude Code tool definitions and system prompt contents
// as of early 2025. They change between Claude Code versions.
//
// NOT live-validated. These are manual estimates from session transcripts
// using ~4 bytes/token. Live calibration (benchmarks/live_test.go) shows
// the bytes/4 heuristic underestimates Claude's tokenizer by ~31%, so
// actual baseline may be higher. We cannot measure the real Claude Code
// baseline via count_tokens because we don't have the exact tool schemas
// Claude Code sends internally.
type ClaudeCodeBaseline struct {
	// Per-tool token estimates (from observable Claude Code tool definitions).
	// Claude Code's built-in tools have much longer descriptions than the
	// equivalent Abbyfile/MCP tool schemas.
	BuiltinTools map[string]int

	// System prompt: instructions, rules, environment context, formatting
	// guidance, git/commit rules, etc. Observable in Claude Code sessions.
	SystemPromptTokens int

	// Total: all builtin tools + system prompt.
	TotalBuiltinTokens int
	TotalBaseTokens    int
}

// EstimateClaudeCodeBaseline returns conservative estimates of Claude Code's
// base context overhead. These are derived from measuring Claude Code tool
// descriptions and system prompt content visible in actual sessions.
func EstimateClaudeCodeBaseline() *ClaudeCodeBaseline {
	// Token estimates based on observable Claude Code v1.x tool definitions.
	// Each tool's full description (with usage rules, examples, constraints)
	// is included in every API request. Claude Code's descriptions are much
	// longer than the minimal schemas Abbyfile agents register via MCP.
	//
	// Method: measured tool descriptions from Claude Code session transcripts,
	// estimated at ~4 bytes/token for the description text.
	// Caveat: bytes/4 underestimates Claude's tokenizer by ~31% (see
	// LiveCorrectionFactor). Actual per-tool costs may be ~1.3x higher.
	tools := map[string]int{
		"Read":             200,  // File reading with offset/limit params, usage rules
		"Write":            200,  // File writing with overwrite warning, usage rules
		"Edit":             300,  // String replacement with indentation preservation rules
		"Bash":             1000, // Massive description: safety rules, git protocol, commit format
		"Glob":             100,  // File pattern matching, usage guidance
		"Grep":             200,  // Regex search with mode selection, usage guidance
		"Agent":            1500, // Agent launcher: types, descriptions, isolation modes
		"WebFetch":         200,  // URL fetching with cache and redirect handling
		"WebSearch":        150,  // Web search with domain filtering, year guidance
		"LSP":              150,  // Language server protocol operations
		"NotebookEdit":     100,  // Jupyter notebook cell editing
		"AskUserQuestion":  200,  // Interactive question with options/previews
		"EnterPlanMode":    300,  // Plan mode with when-to-use guidance
		"ExitPlanMode":     100,  // Plan approval signaling
		"TaskCreate":       200,  // Task list management
		"TaskUpdate":       200,  // Task status updates
		"TaskGet":          100,  // Task retrieval
		"TaskList":         100,  // Task listing
		"EnterWorktree":    100,  // Git worktree isolation
		"Skill":            100,  // Skill invocation
		"TodoWrite":        100,  // Todo list (deprecated alias)
		"ListMcpResources": 50,   // MCP resource listing
		"ReadMcpResource":  50,   // MCP resource reading
	}

	totalTools := 0
	for _, tokens := range tools {
		totalTools += tokens
	}

	// System prompt: Claude Code includes extensive instructions about
	// behavior, tool usage rules, git identity, code style, security,
	// output formatting, etc. Measured at ~12-15K bytes from session
	// transcripts, estimated at ~3,500 tokens.
	systemPrompt := 3500

	return &ClaudeCodeBaseline{
		BuiltinTools:       tools,
		SystemPromptTokens: systemPrompt,
		TotalBuiltinTokens: totalTools,
		TotalBaseTokens:    totalTools + systemPrompt,
	}
}

// MarginalCost represents the additional context cost of loading an Abbyfile agent.
type MarginalCost struct {
	AgentName       string
	AgentTokens     int     // tokens the agent adds
	BaselineTokens  int     // Claude Code's existing overhead
	CombinedTokens  int     // baseline + agent
	MarginalPercent float64 // agent / baseline as percentage increase
	BudgetBefore    float64 // baseline % of 128K
	BudgetAfter     float64 // combined % of 128K
	BudgetDelta     float64 // percentage points added
}

// ComputeMarginalCost calculates how much context an Abbyfile agent adds
// on top of Claude Code's existing overhead.
func ComputeMarginalCost(agentName string, agentTokens int, baseline *ClaudeCodeBaseline) *MarginalCost {
	combined := baseline.TotalBaseTokens + agentTokens
	return &MarginalCost{
		AgentName:       agentName,
		AgentTokens:     agentTokens,
		BaselineTokens:  baseline.TotalBaseTokens,
		CombinedTokens:  combined,
		MarginalPercent: float64(agentTokens) / float64(baseline.TotalBaseTokens) * 100,
		BudgetBefore:    float64(baseline.TotalBaseTokens) / float64(ContextWindow) * 100,
		BudgetAfter:     float64(combined) / float64(ContextWindow) * 100,
		BudgetDelta:     float64(agentTokens) / float64(ContextWindow) * 100,
	}
}

// FormatBaselineAnalysis outputs the baseline analysis as formatted text.
func FormatBaselineAnalysis(baseline *ClaudeCodeBaseline, marginals []*MarginalCost) string {
	var b strings.Builder
	b.WriteString("Claude Code baseline context analysis:\n")
	b.WriteString("(Estimated from observable tool definitions and system prompt)\n\n")

	fmt.Fprintf(&b, "  Claude Code system prompt: ~%d tokens\n", baseline.SystemPromptTokens)
	fmt.Fprintf(&b, "  Claude Code built-in tools (%d): ~%d tokens\n",
		len(baseline.BuiltinTools), baseline.TotalBuiltinTokens)
	fmt.Fprintf(&b, "  Total baseline: ~%d tokens (%.1f%% of 128K)\n\n",
		baseline.TotalBaseTokens,
		float64(baseline.TotalBaseTokens)/float64(ContextWindow)*100)

	if len(marginals) > 0 {
		b.WriteString("  Marginal cost of adding Abbyfile agents:\n")
		for _, m := range marginals {
			fmt.Fprintf(&b, "    %-20s +%d tokens (+%.1f%% over baseline, %.1f%% -> %.1f%% of 128K)\n",
				m.AgentName, m.AgentTokens, m.MarginalPercent, m.BudgetBefore, m.BudgetAfter)
		}
	}

	return b.String()
}
