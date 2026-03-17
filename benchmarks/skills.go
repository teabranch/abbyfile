package benchmarks

import (
	"fmt"
	"strings"
)

// SkillTier represents one level of Agent Skills progressive disclosure.
// Agent Skills (Anthropic's pattern) load context in tiers: metadata first,
// full prompt on activation, and optionally referenced files.
type SkillTier struct {
	Name   string `json:"name"`   // "metadata", "loaded", "with-files"
	Tokens int    `json:"tokens"` // estimated tokens at this tier
	Label  string `json:"label"`  // human-readable description
}

// SkillCost models the context cost and capability set of an Agent Skill.
type SkillCost struct {
	Name  string      `json:"name"`
	Tiers []SkillTier `json:"tiers"`
	// What skills provide.
	HasTools        bool `json:"has_tools"`
	HasMemory       bool `json:"has_memory"`
	HasVersioning   bool `json:"has_versioning"`
	HasDistribution bool `json:"has_distribution"`
}

// AbbyfileCost models the context cost and capability set of an Abbyfile agent.
type AbbyfileCost struct {
	Name         string `json:"name"`
	ToolTokens   int    `json:"tool_tokens"`
	PromptTokens int    `json:"prompt_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	// What Abbyfile provides.
	HasTools        bool `json:"has_tools"`
	HasMemory       bool `json:"has_memory"`
	HasVersioning   bool `json:"has_versioning"`
	HasDistribution bool `json:"has_distribution"`
}

// SkillsComparison holds a side-by-side analysis of skills vs Abbyfile.
type SkillsComparison struct {
	Skill        SkillCost    `json:"skill"`
	Abbyfile     AbbyfileCost `json:"abbyfile"`
	TokenDelta   int          `json:"token_delta"`   // abbyfile - skill loaded tier
	TokenRatio   float64      `json:"token_ratio"`   // abbyfile / skill loaded tier
	FeatureDelta []string     `json:"feature_delta"` // capabilities Abbyfile adds
}

// DefaultSkillTiers returns the three standard progressive disclosure tiers
// for Agent Skills, based on Anthropic's engineering blog and docs.
//
// The "loaded" tier was calibrated against Claude's count_tokens API.
// Live measurement of community agent prompts (cli-developer: 816T,
// code-reviewer: ~800T, debugger: ~850T) shows ~800T for a typical
// focused agent prompt. The original 3,000T estimate was based on
// blog post examples of larger, more general skill prompts.
//
// Sources:
//   - https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills
//   - Live validation via Anthropic count_tokens API (benchmarks/live_test.go)
func DefaultSkillTiers() []SkillTier {
	return []SkillTier{
		{
			Name:   "metadata",
			Tokens: 100,
			Label:  "Name + description only (always in context)",
		},
		{
			Name:   "loaded",
			Tokens: 800,
			Label:  "Full skill prompt loaded into context (live-calibrated)",
		},
		{
			Name:   "with-files",
			Tokens: 5000,
			Label:  "Skill prompt + referenced file contents",
		},
	}
}

// EstimateSkillCost creates a SkillCost with the given tiers.
// Skills provide context-only instructions — no executable tools,
// no persistent memory, no versioning, no distribution.
func EstimateSkillCost(name string, tiers []SkillTier) *SkillCost {
	return &SkillCost{
		Name:            name,
		Tiers:           tiers,
		HasTools:        false,
		HasMemory:       false,
		HasVersioning:   false,
		HasDistribution: false,
	}
}

// CompareSkillsVsAbbyfile compares an Agent Skill against an Abbyfile agent,
// using the "loaded" tier (full prompt in context) as the comparison point.
func CompareSkillsVsAbbyfile(skill *SkillCost, abbyfile *AbbyfileCost) *SkillsComparison {
	// Find loaded tier for comparison.
	loadedTokens := 0
	for _, t := range skill.Tiers {
		if t.Name == "loaded" {
			loadedTokens = t.Tokens
			break
		}
	}

	ratio := 0.0
	if loadedTokens > 0 {
		ratio = float64(abbyfile.TotalTokens) / float64(loadedTokens)
	}

	var featureDelta []string
	if abbyfile.HasTools && !skill.HasTools {
		featureDelta = append(featureDelta, "executable tools (MCP)")
	}
	if abbyfile.HasMemory && !skill.HasMemory {
		featureDelta = append(featureDelta, "persistent memory")
	}
	if abbyfile.HasVersioning && !skill.HasVersioning {
		featureDelta = append(featureDelta, "semantic versioning")
	}
	if abbyfile.HasDistribution && !skill.HasDistribution {
		featureDelta = append(featureDelta, "one-command distribution")
	}

	return &SkillsComparison{
		Skill:        *skill,
		Abbyfile:     *abbyfile,
		TokenDelta:   abbyfile.TotalTokens - loadedTokens,
		TokenRatio:   ratio,
		FeatureDelta: featureDelta,
	}
}

// FormatSkillsComparison outputs the skills vs Abbyfile analysis as text.
func FormatSkillsComparison(cmp *SkillsComparison) string {
	var b strings.Builder
	b.WriteString("Agent Skills vs Abbyfile comparison:\n\n")

	b.WriteString("  Agent Skills progressive disclosure tiers:\n")
	for _, t := range cmp.Skill.Tiers {
		fmt.Fprintf(&b, "    %-12s ~%dT  (%s)\n", t.Name+":", t.Tokens, t.Label)
	}

	fmt.Fprintf(&b, "\n  Abbyfile agent (%s):\n", cmp.Abbyfile.Name)
	fmt.Fprintf(&b, "    Tool schemas:  ~%dT\n", cmp.Abbyfile.ToolTokens)
	fmt.Fprintf(&b, "    System prompt: ~%dT\n", cmp.Abbyfile.PromptTokens)
	fmt.Fprintf(&b, "    Total:         ~%dT\n", cmp.Abbyfile.TotalTokens)

	b.WriteString("\n  Token comparison (vs loaded skill tier):\n")
	fmt.Fprintf(&b, "    Skill loaded:    ~%dT\n", cmp.Skill.Tiers[1].Tokens)
	fmt.Fprintf(&b, "    Abbyfile total: ~%dT\n", cmp.Abbyfile.TotalTokens)
	fmt.Fprintf(&b, "    Delta:           %+dT (%.1fx)\n", cmp.TokenDelta, cmp.TokenRatio)

	b.WriteString("\n  Capabilities Abbyfile adds over Skills:\n")
	for _, f := range cmp.FeatureDelta {
		fmt.Fprintf(&b, "    + %s\n", f)
	}

	b.WriteString("\n  Capabilities comparison:\n")
	fmt.Fprintf(&b, "    %-25s  Skills  Abbyfile\n", "Feature")
	fmt.Fprintf(&b, "    %-25s  ------  ---------\n", strings.Repeat("-", 25))
	fmt.Fprintf(&b, "    %-25s  %-6s  %-9s\n", "Executable tools (MCP)", boolMark(cmp.Skill.HasTools), boolMark(cmp.Abbyfile.HasTools))
	fmt.Fprintf(&b, "    %-25s  %-6s  %-9s\n", "Persistent memory", boolMark(cmp.Skill.HasMemory), boolMark(cmp.Abbyfile.HasMemory))
	fmt.Fprintf(&b, "    %-25s  %-6s  %-9s\n", "Semantic versioning", boolMark(cmp.Skill.HasVersioning), boolMark(cmp.Abbyfile.HasVersioning))
	fmt.Fprintf(&b, "    %-25s  %-6s  %-9s\n", "One-command distribution", boolMark(cmp.Skill.HasDistribution), boolMark(cmp.Abbyfile.HasDistribution))

	return b.String()
}

func boolMark(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
