package benchmarks

import (
	"fmt"
	"strings"
)

// ApproachSummary captures the cost model and capabilities of one approach.
type ApproachSummary struct {
	Name         string   `json:"name"`
	TokenCost    string   `json:"token_cost"`    // human-readable cost description
	PerTurnCost  int      `json:"per_turn_cost"` // tokens per API turn (or per invocation)
	Capabilities []string `json:"capabilities"`  // what it provides
	Limitations  []string `json:"limitations"`   // what it lacks
}

// ThreeWayComparison holds the full skills vs sub-agents vs Abbyfile analysis.
type ThreeWayComparison struct {
	Skill    ApproachSummary `json:"skill"`
	SubAgent ApproachSummary `json:"sub_agent"`
	Abbyfile ApproachSummary `json:"abbyfile"`
}

// CompareThreeWay builds a three-way comparison using measured Abbyfile costs
// and estimated costs for skills and sub-agents.
//
// Cost model per turn:
//   - Skills: loaded tokens (text in context, no tool schemas)
//   - Sub-agents: baseline (~9.2K) + tools + prompt per invocation (separate API call)
//   - Abbyfile: tools + prompt marginal on existing context (same API call)
func CompareThreeWay(abbyfileToolTokens, abbyfilePromptTokens int) *ThreeWayComparison {
	baseline := EstimateClaudeCodeBaseline()
	skillTiers := DefaultSkillTiers()

	// Find loaded tier cost.
	skillLoadedTokens := 0
	for _, t := range skillTiers {
		if t.Name == "loaded" {
			skillLoadedTokens = t.Tokens
			break
		}
	}

	abbyfilePerTurn := abbyfileToolTokens + abbyfilePromptTokens
	subAgentPerCall := baseline.TotalBaseTokens + abbyfileToolTokens + abbyfilePromptTokens

	return &ThreeWayComparison{
		Skill: ApproachSummary{
			Name:        "Agent Skills",
			TokenCost:   fmt.Sprintf("~%dT metadata, ~%dT loaded", skillTiers[0].Tokens, skillLoadedTokens),
			PerTurnCost: skillLoadedTokens,
			Capabilities: []string{
				"Progressive disclosure (lazy loading)",
				"Markdown-only (maximum portability)",
				"Zero infrastructure",
			},
			Limitations: []string{
				"No executable tools",
				"No persistent memory",
				"No versioning or distribution",
				"No validation or testing framework",
			},
		},
		SubAgent: ApproachSummary{
			Name:        "Sub-agents",
			TokenCost:   fmt.Sprintf("~%dT per invocation (baseline re-paid)", subAgentPerCall),
			PerTurnCost: subAgentPerCall,
			Capabilities: []string{
				"Context isolation (separate window)",
				"Can use any tools (MCP, built-in)",
				"Independent reasoning",
			},
			Limitations: []string{
				"Re-pays ~9.2K baseline per call",
				"No persistent memory across calls",
				"No versioning or distribution",
				"Results opaque to parent context",
			},
		},
		Abbyfile: ApproachSummary{
			Name:        "Abbyfile",
			TokenCost:   fmt.Sprintf("~%dT marginal (tools + prompt)", abbyfilePerTurn),
			PerTurnCost: abbyfilePerTurn,
			Capabilities: []string{
				"Executable tools via MCP",
				"Persistent memory across conversations",
				"Semantic versioning",
				"One-command distribution",
				"Validation and testing framework",
				"Same context window (no baseline re-payment)",
			},
			Limitations: []string{
				"No context isolation (shares main window)",
				"Requires Go toolchain for building",
			},
		},
	}
}

// FormatThreeWayComparison outputs the full three-way analysis as text.
func FormatThreeWayComparison(cmp *ThreeWayComparison) string {
	var b strings.Builder
	b.WriteString("Three-way comparison: Skills vs Sub-agents vs Abbyfile\n\n")

	// Cost comparison table.
	b.WriteString("  Per-turn/per-call cost model:\n")
	fmt.Fprintf(&b, "    %-15s  %s\n", "Agent Skills:", cmp.Skill.TokenCost)
	fmt.Fprintf(&b, "    %-15s  %s\n", "Sub-agents:", cmp.SubAgent.TokenCost)
	fmt.Fprintf(&b, "    %-15s  %s\n", "Abbyfile:", cmp.Abbyfile.TokenCost)

	// Feature matrix.
	b.WriteString("\n  Feature matrix:\n")
	fmt.Fprintf(&b, "    %-30s  %-14s  %-14s  %-14s\n",
		"Feature", "Skills", "Sub-agents", "Abbyfile")
	fmt.Fprintf(&b, "    %-30s  %-14s  %-14s  %-14s\n",
		strings.Repeat("-", 30), strings.Repeat("-", 14), strings.Repeat("-", 14), strings.Repeat("-", 14))

	features := []struct {
		name                      string
		skill, subagent, abbyfile string
	}{
		{"Context cost", fmt.Sprintf("~%dT loaded", cmp.Skill.PerTurnCost), fmt.Sprintf("~%dT/call", cmp.SubAgent.PerTurnCost), fmt.Sprintf("~%dT marginal", cmp.Abbyfile.PerTurnCost)},
		{"Executable tools", "no", "yes (inherited)", "yes (MCP)"},
		{"Persistent memory", "no", "no", "yes"},
		{"Versioning", "no", "no", "semver"},
		{"Distribution", "folder copy", "no", "abby install"},
		{"Context isolation", "no", "yes", "no"},
		{"Cost model", "text in context", "baseline/call", "marginal/turn"},
		{"Validation/testing", "no", "no", "yes"},
	}

	for _, f := range features {
		fmt.Fprintf(&b, "    %-30s  %-14s  %-14s  %-14s\n",
			f.name, f.skill, f.subagent, f.abbyfile)
	}

	return b.String()
}

// FormatThreeWaySummary returns a concise positioning statement.
func FormatThreeWaySummary() string {
	return `Positioning summary:

  Agent Skills are the lightest option: markdown files with progressive
  disclosure, ideal for context-only instructions without tools or memory.

  Sub-agents provide context isolation but re-pay Claude Code's ~9.2K
  baseline on every invocation — expensive for repeated tool use.

  Abbyfile is the sweet spot: executable tools, persistent memory, and
  versioned distribution at marginal context cost (~1-2K tokens) on the
  existing context window. No baseline re-payment, no separate API calls.

  These are not mutually exclusive. An Abbyfile agent can coexist with
  skills in the same project, and sub-agents can invoke Abbyfile agents'
  MCP tools.`
}
