package benchmarks

import (
	"fmt"
	"strings"
)

// SubAgentInvocation models the per-call cost of a Claude Code sub-agent.
// Each sub-agent invocation opens a separate context window and re-pays
// Claude Code's baseline overhead (system prompt + built-in tools).
//
// Source: https://code.claude.com/docs/en/sub-agents
// Real-world data: https://github.com/anthropics/claude-code/issues/11364
// (67.3K tokens for 7 MCP servers, ~7x team multiplier)
type SubAgentInvocation struct {
	BaselineTokens int `json:"baseline_tokens"` // Claude Code overhead re-paid each call (~9,200)
	ToolTokens     int `json:"tool_tokens"`     // sub-agent's own tools
	PromptTokens   int `json:"prompt_tokens"`   // sub-agent's system prompt
	PerCallTokens  int `json:"per_call_tokens"` // total per invocation
}

// SubAgentProjection holds cumulative cost at a given invocation count.
type SubAgentProjection struct {
	Invocations      int `json:"invocations"`
	PerCallTokens    int `json:"per_call_tokens"`
	CumulativeTokens int `json:"cumulative_tokens"`
}

// SubAgentComparison holds side-by-side sub-agent vs Abbyfile projections.
type SubAgentComparison struct {
	SubAgent        SubAgentInvocation       `json:"sub_agent"`
	AbbyfilePerTurn int                      `json:"abbyfile_per_turn"` // marginal cost per turn
	Projections     []SubAgentProjectionPair `json:"projections"`
}

// SubAgentProjectionPair holds one data point in the comparison.
type SubAgentProjectionPair struct {
	Invocations        int     `json:"invocations"`
	SubAgentCumulative int     `json:"sub_agent_cumulative"`
	AbbyfileCumulative int     `json:"abbyfile_cumulative"`
	Ratio              float64 `json:"ratio"` // sub-agent / abbyfile
}

// EstimateSubAgentInvocation calculates the per-call cost of a sub-agent.
// Each invocation starts a new context window that includes Claude Code's
// full baseline (system prompt + all built-in tools) plus the sub-agent's
// own tools and prompt.
func EstimateSubAgentInvocation(toolTokens, promptTokens int) *SubAgentInvocation {
	baseline := EstimateClaudeCodeBaseline()
	perCall := baseline.TotalBaseTokens + toolTokens + promptTokens
	return &SubAgentInvocation{
		BaselineTokens: baseline.TotalBaseTokens,
		ToolTokens:     toolTokens,
		PromptTokens:   promptTokens,
		PerCallTokens:  perCall,
	}
}

// ProjectSubAgentCost projects cumulative token cost over multiple invocations.
func ProjectSubAgentCost(inv *SubAgentInvocation, counts []int) []SubAgentProjection {
	projections := make([]SubAgentProjection, len(counts))
	for i, n := range counts {
		projections[i] = SubAgentProjection{
			Invocations:      n,
			PerCallTokens:    inv.PerCallTokens,
			CumulativeTokens: inv.PerCallTokens * n,
		}
	}
	return projections
}

// DefaultInvocationCounts returns the standard set of invocation counts.
func DefaultInvocationCounts() []int {
	return []int{1, 3, 5, 10, 20}
}

// CompareSubAgentVsAbbyfile compares sub-agent cumulative cost against
// Abbyfile's marginal per-turn cost over multiple invocations.
//
// Key insight: sub-agents re-pay the full Claude Code baseline on each
// invocation (separate context window). Abbyfile agents add marginal
// tokens to the existing context (same API call, no baseline re-payment).
func CompareSubAgentVsAbbyfile(inv *SubAgentInvocation, abbyfilePerTurn int, counts []int) *SubAgentComparison {
	cmp := &SubAgentComparison{
		SubAgent:        *inv,
		AbbyfilePerTurn: abbyfilePerTurn,
	}

	for _, n := range counts {
		subCum := inv.PerCallTokens * n
		agfCum := abbyfilePerTurn * n

		ratio := 0.0
		if agfCum > 0 {
			ratio = float64(subCum) / float64(agfCum)
		}

		cmp.Projections = append(cmp.Projections, SubAgentProjectionPair{
			Invocations:        n,
			SubAgentCumulative: subCum,
			AbbyfileCumulative: agfCum,
			Ratio:              ratio,
		})
	}

	return cmp
}

// FormatSubAgentBreakdown outputs a per-call cost breakdown.
func FormatSubAgentBreakdown(inv *SubAgentInvocation) string {
	var b strings.Builder
	b.WriteString("Sub-agent per-invocation cost breakdown:\n")
	b.WriteString("(Each sub-agent call opens a new context window)\n\n")

	fmt.Fprintf(&b, "  Claude Code baseline (re-paid):  ~%dT\n", inv.BaselineTokens)
	fmt.Fprintf(&b, "  Sub-agent tools:                 ~%dT\n", inv.ToolTokens)
	fmt.Fprintf(&b, "  Sub-agent prompt:                ~%dT\n", inv.PromptTokens)
	fmt.Fprintf(&b, "  Total per invocation:            ~%dT\n", inv.PerCallTokens)

	return b.String()
}

// FormatSubAgentComparison outputs the sub-agent vs Abbyfile projection table.
func FormatSubAgentComparison(cmp *SubAgentComparison) string {
	var b strings.Builder
	b.WriteString("Sub-agent vs Abbyfile cumulative cost projection:\n")
	b.WriteString("(Sub-agents re-pay baseline per call; Abbyfile adds marginal tokens)\n\n")

	// Header.
	fmt.Fprintf(&b, "  %-12s", "Invocations")
	for _, p := range cmp.Projections {
		fmt.Fprintf(&b, "  %10s", fmt.Sprintf("%d", p.Invocations))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %-12s", strings.Repeat("-", 12))
	for range cmp.Projections {
		fmt.Fprintf(&b, "  %10s", "----------")
	}
	b.WriteString("\n")

	// Sub-agent row.
	fmt.Fprintf(&b, "  %-12s", "Sub-agent")
	for _, p := range cmp.Projections {
		fmt.Fprintf(&b, "  %9dT", p.SubAgentCumulative)
	}
	b.WriteString("\n")

	// Abbyfile row.
	fmt.Fprintf(&b, "  %-12s", "Abbyfile")
	for _, p := range cmp.Projections {
		fmt.Fprintf(&b, "  %9dT", p.AbbyfileCumulative)
	}
	b.WriteString("\n")

	// Ratio row.
	fmt.Fprintf(&b, "  %-12s", "Ratio")
	for _, p := range cmp.Projections {
		fmt.Fprintf(&b, "  %9.1fx", p.Ratio)
	}
	b.WriteString("\n")

	return b.String()
}
