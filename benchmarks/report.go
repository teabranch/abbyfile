package benchmarks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ArticleReference contains the token cost numbers from the reference article
// (Jannik Reinhard's "MCP servers are context hogs").
var ArticleReference = struct {
	GitHubMCP93Tools    int // ~55,000 tokens for 93 tools
	EnterpriseMCPStack  int // ~150,000 tokens for full enterprise stack
	ContextWindowSize   int // 128K tokens
	GitHubToolCount     int // 93 tools
	GitHubBudgetPercent float64
	EnterpriseBudgetPct float64
}{
	GitHubMCP93Tools:    55_000,
	EnterpriseMCPStack:  150_000,
	ContextWindowSize:   ContextWindow,
	GitHubToolCount:     93,
	GitHubBudgetPercent: 43.0,
	EnterpriseBudgetPct: 117.0,
}

// AgentMeasurement holds the combined measurement for one agent.
type AgentMeasurement struct {
	Name         string  `json:"name"`
	ToolCount    int     `json:"tool_count"`
	SchemaBytes  int     `json:"schema_bytes"`
	SchemaTokens int     `json:"schema_tokens"`
	PromptBytes  int     `json:"prompt_bytes"`
	PromptTokens int     `json:"prompt_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	BudgetPct    float64 `json:"budget_pct"`
}

// BenchmarkReport holds all benchmark results for output.
type BenchmarkReport struct {
	ProjectAgents   []AgentMeasurement `json:"project_agents,omitempty"`
	CommunityAgents []AgentMeasurement `json:"community_agents,omitempty"`
	ScalingCurve    []ScalingPoint     `json:"scaling_curve,omitempty"`
}

// JSON returns the report as formatted JSON.
func (r *BenchmarkReport) JSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling report: %w", err)
	}
	return string(data), nil
}

// Text returns the report as a formatted text table.
func (r *BenchmarkReport) Text() string {
	var b strings.Builder

	b.WriteString("=== Abbyfile MCP Token Cost Analysis ===\n\n")

	// Project agents.
	if len(r.ProjectAgents) > 0 {
		b.WriteString("Per-agent measurements (auto-discovered from Abbyfile):\n")
		combinedTokens := 0
		for _, a := range r.ProjectAgents {
			fmt.Fprintf(&b, "  %-20s (%2d tools): ~%5d schema + ~%5d prompt = ~%5d total (%.1f%% of 128K)\n",
				a.Name, a.ToolCount, a.SchemaTokens, a.PromptTokens, a.TotalTokens, a.BudgetPct)
			combinedTokens += a.TotalTokens
		}
		combinedPct := float64(combinedTokens) / float64(ContextWindow) * 100
		fmt.Fprintf(&b, "\nCombined (all %d project agents loaded simultaneously):\n", len(r.ProjectAgents))
		fmt.Fprintf(&b, "  %d agents: ~%d tokens (%.1f%% of 128K)\n\n", len(r.ProjectAgents), combinedTokens, combinedPct)
	}

	// Community agents.
	if len(r.CommunityAgents) > 0 {
		b.WriteString("Community agent baselines (awesome-claude-code-subagents):\n")
		combinedTokens := 0
		for _, a := range r.CommunityAgents {
			fmt.Fprintf(&b, "  %-24s (%2d tools): ~%5d schema + ~%5d prompt = ~%5d total (%.1f%% of 128K)\n",
				a.Name, a.ToolCount, a.SchemaTokens, a.PromptTokens, a.TotalTokens, a.BudgetPct)
			combinedTokens += a.TotalTokens
		}
		combinedPct := float64(combinedTokens) / float64(ContextWindow) * 100
		fmt.Fprintf(&b, "\nCombined (all %d community agents):\n", len(r.CommunityAgents))
		fmt.Fprintf(&b, "  %d agents: ~%d tokens (%.1f%% of 128K)\n\n", len(r.CommunityAgents), combinedTokens, combinedPct)
	}

	// Scaling curve.
	if len(r.ScalingCurve) > 0 {
		b.WriteString("Scaling curve (synthetic):\n")
		for _, p := range r.ScalingCurve {
			label := ""
			if p.ToolCount == 15 {
				label = "  <- recommended max"
			} else if p.ToolCount == 50 {
				label = "  <- anti-pattern"
			}
			fmt.Fprintf(&b, "  %2d tools: %6d tokens  (%4.1f%% of 128K)%s\n",
				p.ToolCount, p.SchemaTokens, p.BudgetPct, label)
		}
		b.WriteString("\n")
	}

	// Comparison with article.
	b.WriteString("vs Article (Jannik Reinhard):\n")
	fmt.Fprintf(&b, "  GitHub MCP (93 tools):  ~%d tokens (%.0f%% of 128K)\n",
		ArticleReference.GitHubMCP93Tools, ArticleReference.GitHubBudgetPercent)

	if len(r.ScalingCurve) > 0 {
		// Find the 10-tool measurement for comparison.
		for _, p := range r.ScalingCurve {
			if p.ToolCount == 10 {
				ratio := float64(ArticleReference.GitHubMCP93Tools) / float64(p.SchemaTokens)
				fmt.Fprintf(&b, "  Abbyfile (10 tools):    ~%d tokens (%.1f%% of 128K) -> %.0fx smaller\n",
					p.SchemaTokens, p.BudgetPct, ratio)
				break
			}
		}
	}

	if len(r.CommunityAgents) > 0 {
		combinedTokens := 0
		for _, a := range r.CommunityAgents {
			combinedTokens += a.TotalTokens
		}
		combinedPct := float64(combinedTokens) / float64(ContextWindow) * 100
		ratio := float64(ArticleReference.EnterpriseMCPStack) / float64(combinedTokens)
		fmt.Fprintf(&b, "  %d community agents:     ~%d tokens (%.1f%%) vs enterprise MCP stack: ~%d (%.0f%%) -> %.0fx smaller\n",
			len(r.CommunityAgents), combinedTokens, combinedPct,
			ArticleReference.EnterpriseMCPStack, ArticleReference.EnterpriseBudgetPct, ratio)
	}

	return b.String()
}

// FormatAntiPatternAnalysis returns a text analysis showing where tool count
// crosses budget thresholds.
func FormatAntiPatternAnalysis(points []ScalingPoint) string {
	var b strings.Builder
	b.WriteString("Anti-pattern analysis (context budget thresholds):\n\n")

	thresholds := []struct {
		pct   float64
		label string
	}{
		{5.0, "reasonable ceiling"},
		{10.0, "anti-pattern warning"},
		{20.0, "severe budget pressure"},
	}

	for _, t := range thresholds {
		crossed := false
		for _, p := range points {
			if p.BudgetPct >= t.pct && !crossed {
				fmt.Fprintf(&b, "  %.0f%% budget (%s): crossed at %d tools (~%d tokens)\n",
					t.pct, t.label, p.ToolCount, p.SchemaTokens)
				crossed = true
			}
		}
		if !crossed {
			fmt.Fprintf(&b, "  %.0f%% budget (%s): not reached in measured range\n", t.pct, t.label)
		}
	}

	return b.String()
}
