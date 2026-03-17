package benchmarks_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/benchmarks"
	"github.com/teabranch/abbyfile/pkg/builtins"
	"github.com/teabranch/abbyfile/pkg/definition"
)

// skipIfNoAPIKey skips the test if ANTHROPIC_API_KEY is not set.
func skipIfNoAPIKey(t *testing.T) *benchmarks.AnthropicClient {
	t.Helper()
	client, ok := benchmarks.NewAnthropicClient()
	if !ok {
		t.Skip("ANTHROPIC_API_KEY not set — skipping live test")
	}
	return client
}

// loadFirstCommunityAgent loads the first .md agent from testdata/community.
func loadFirstCommunityAgent(t *testing.T) *definition.AgentDef {
	t.Helper()
	communityDir := filepath.Join("testdata", "community")
	entries, err := os.ReadDir(communityDir)
	if err != nil {
		t.Fatalf("reading community dir: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		def, err := definition.ParseAgentMD(path)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		return def
	}
	t.Fatal("no community agent found")
	return nil
}

// loadAllCommunityAgents loads all .md agents from testdata/community.
func loadAllCommunityAgents(t *testing.T) []*definition.AgentDef {
	t.Helper()
	communityDir := filepath.Join("testdata", "community")
	entries, err := os.ReadDir(communityDir)
	if err != nil {
		t.Fatalf("reading community dir: %v", err)
	}
	var agents []*definition.AgentDef
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		def, err := definition.ParseAgentMD(path)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		agents = append(agents, def)
	}
	return agents
}

// TestLiveAbbyfileTokenCount measures a community agent's exact token count
// via the Anthropic count_tokens API and compares with heuristic and BPE estimates.
func TestLiveAbbyfileTokenCount(t *testing.T) {
	client := skipIfNoAPIKey(t)
	agentDef := loadFirstCommunityAgent(t)

	// Resolve agent's tools and convert to Anthropic format.
	toolDefs, err := builtins.ForNames(agentDef.Tools)
	if err != nil {
		t.Fatalf("resolving tools for %s: %v", agentDef.Name, err)
	}
	anthropicTools := benchmarks.ToolsToAnthropic(toolDefs)

	// Call count_tokens with agent's system prompt + tools.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	liveTokens, err := client.CountTokens(ctx, benchmarks.CountTokensRequest{
		Messages: []benchmarks.AnthropicMsg{{Role: "user", Content: "x"}},
		System:   agentDef.PromptBody,
		Tools:    anthropicTools,
	})
	if err != nil {
		t.Fatalf("count_tokens API call failed: %v", err)
	}

	// Get heuristic and BPE estimates for comparison.
	m := benchmarks.MeasureTools(toolDefs)
	heuristicTokens := m.SchemaTokens + benchmarks.EstimateTokens(agentDef.PromptBody)

	bpeTokens := 0
	bpe, bpeErr := benchmarks.NewBPECounter()
	if bpeErr == nil {
		bpeM := benchmarks.MeasureToolsWith(toolDefs, bpe)
		bpeTokens = bpeM.SchemaTokens + bpe.Count(agentDef.PromptBody)
	}

	t.Logf("Agent: %s (%d tools)", agentDef.Name, len(toolDefs))
	t.Logf("  Live (count_tokens):  %d tokens", liveTokens)
	t.Logf("  Heuristic (bytes/4):  %d tokens", heuristicTokens)
	if bpeTokens > 0 {
		t.Logf("  BPE (cl100k_base):    %d tokens", bpeTokens)
	}

	// Log correction factors.
	if heuristicTokens > 0 {
		t.Logf("  Correction: live/heuristic = %.2f", float64(liveTokens)/float64(heuristicTokens))
	}
	if bpeTokens > 0 {
		t.Logf("  Correction: live/BPE = %.2f", float64(liveTokens)/float64(bpeTokens))
	}

	// Assert heuristic is within 40% of live count.
	// BPE (cl100k_base) gets a wider 70% tolerance — it's GPT-4's tokenizer,
	// not Claude's. Live calibration shows cl100k_base consistently underestimates
	// Claude's token count by ~67%.
	heuristicRatio := float64(heuristicTokens) / float64(liveTokens)
	if heuristicRatio < 0.60 || heuristicRatio > 1.40 {
		t.Errorf("heuristic estimate (%d) differs from live (%d) by more than 40%% (ratio: %.2f)",
			heuristicTokens, liveTokens, heuristicRatio)
	}
	if bpeTokens > 0 {
		bpeRatio := float64(bpeTokens) / float64(liveTokens)
		if bpeRatio < 0.30 || bpeRatio > 1.70 {
			t.Errorf("BPE estimate (%d) differs from live (%d) by more than 70%% (ratio: %.2f)",
				bpeTokens, liveTokens, bpeRatio)
		}
	}
}

// TestLiveSubAgentTokenCount measures the full sub-agent invocation cost:
// Claude Code baseline tools + agent's tools + system prompt.
func TestLiveSubAgentTokenCount(t *testing.T) {
	client := skipIfNoAPIKey(t)
	agentDef := loadFirstCommunityAgent(t)

	// Build Claude Code baseline tools as Anthropic format.
	baselineTools := benchmarks.BaselineToolsToAnthropic()

	// Add agent's own tools.
	toolDefs, err := builtins.ForNames(agentDef.Tools)
	if err != nil {
		t.Fatalf("resolving tools for %s: %v", agentDef.Name, err)
	}
	agentTools := benchmarks.ToolsToAnthropic(toolDefs)
	allTools := append(baselineTools, agentTools...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	liveTokens, err := client.CountTokens(ctx, benchmarks.CountTokensRequest{
		Messages: []benchmarks.AnthropicMsg{{Role: "user", Content: "x"}},
		System:   agentDef.PromptBody,
		Tools:    allTools,
	})
	if err != nil {
		t.Fatalf("count_tokens API call failed: %v", err)
	}

	// Compare with our estimate.
	m := benchmarks.MeasureTools(toolDefs)
	estimated := benchmarks.EstimateSubAgentInvocation(m.SchemaTokens, benchmarks.EstimateTokens(agentDef.PromptBody))

	t.Logf("Sub-agent invocation for %s:", agentDef.Name)
	t.Logf("  Live (count_tokens):  %d tokens", liveTokens)
	t.Logf("  Estimated:            %d tokens", estimated.PerCallTokens)
	t.Logf("    Baseline:           %d tokens", estimated.BaselineTokens)
	t.Logf("    Agent tools:        %d tokens", estimated.ToolTokens)
	t.Logf("    Agent prompt:       %d tokens", estimated.PromptTokens)
	t.Logf("  Correction: live/estimated = %.2f", float64(liveTokens)/float64(estimated.PerCallTokens))
}

// TestLiveThreeWayValidation measures all three configurations (skill, sub-agent,
// Abbyfile) with the live API and verifies the ranking holds.
func TestLiveThreeWayValidation(t *testing.T) {
	client := skipIfNoAPIKey(t)
	agentDef := loadFirstCommunityAgent(t)

	toolDefs, err := builtins.ForNames(agentDef.Tools)
	if err != nil {
		t.Fatalf("resolving tools for %s: %v", agentDef.Name, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Skill: just the system prompt (natural language, no tool schemas).
	skillTokens, err := client.CountTokens(ctx, benchmarks.CountTokensRequest{
		Messages: []benchmarks.AnthropicMsg{{Role: "user", Content: "x"}},
		System:   agentDef.PromptBody,
	})
	if err != nil {
		t.Fatalf("skill count_tokens failed: %v", err)
	}

	// 2. Sub-agent: baseline tools + agent tools + system prompt.
	baselineTools := benchmarks.BaselineToolsToAnthropic()
	agentTools := benchmarks.ToolsToAnthropic(toolDefs)
	allTools := append(baselineTools, agentTools...)

	subAgentTokens, err := client.CountTokens(ctx, benchmarks.CountTokensRequest{
		Messages: []benchmarks.AnthropicMsg{{Role: "user", Content: "x"}},
		System:   agentDef.PromptBody,
		Tools:    allTools,
	})
	if err != nil {
		t.Fatalf("sub-agent count_tokens failed: %v", err)
	}

	// 3. Abbyfile: just agent tools + system prompt (marginal cost).
	abbyfileTokens, err := client.CountTokens(ctx, benchmarks.CountTokensRequest{
		Messages: []benchmarks.AnthropicMsg{{Role: "user", Content: "x"}},
		System:   agentDef.PromptBody,
		Tools:    agentTools,
	})
	if err != nil {
		t.Fatalf("abby count_tokens failed: %v", err)
	}

	// Get estimated values for comparison.
	m := benchmarks.MeasureTools(toolDefs)
	promptTokens := benchmarks.EstimateTokens(agentDef.PromptBody)
	cmp := benchmarks.CompareThreeWay(m.SchemaTokens, promptTokens)

	t.Logf("Three-way live validation for %s:", agentDef.Name)
	t.Logf("")
	t.Logf("  %-14s  %8s  %8s  %s", "Approach", "Live", "Estimated", "Correction")
	t.Logf("  %-14s  %8s  %8s  %s", "--------------", "--------", "---------", "----------")
	t.Logf("  %-14s  %8d  %8d  %.2f", "Skill", skillTokens, cmp.Skill.PerTurnCost, float64(skillTokens)/float64(cmp.Skill.PerTurnCost))
	t.Logf("  %-14s  %8d  %8d  %.2f", "Sub-agent", subAgentTokens, cmp.SubAgent.PerTurnCost, float64(subAgentTokens)/float64(cmp.SubAgent.PerTurnCost))
	t.Logf("  %-14s  %8d  %8d  %.2f", "Abbyfile", abbyfileTokens, cmp.Abbyfile.PerTurnCost, float64(abbyfileTokens)/float64(cmp.Abbyfile.PerTurnCost))

	// Verify ranking: Abbyfile < Skill loaded < Sub-agent.
	// Note: Abbyfile includes tool schemas so it may be larger than a pure skill (prompt only).
	// The key ranking is: Abbyfile << Sub-agent.
	if abbyfileTokens >= subAgentTokens {
		t.Errorf("ranking violated: Abbyfile (%d) should be less than Sub-agent (%d)",
			abbyfileTokens, subAgentTokens)
	}
	t.Logf("")
	t.Logf("  Ranking confirmed: Abbyfile (%d) < Sub-agent (%d)", abbyfileTokens, subAgentTokens)
	t.Logf("  Sub-agent overhead: %.1fx Abbyfile", float64(subAgentTokens)/float64(abbyfileTokens))
}

// TestLiveCalibration measures all 5 community agents with the live API
// and computes average correction factors vs heuristic and BPE.
func TestLiveCalibration(t *testing.T) {
	client := skipIfNoAPIKey(t)
	agents := loadAllCommunityAgents(t)

	bpe, bpeErr := benchmarks.NewBPECounter()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var heuristicRatios, bpeRatios []float64

	t.Log("Live calibration across community agents:")
	t.Logf("")
	if bpeErr == nil {
		t.Logf("  %-24s  %8s  %8s  %8s  %8s  %8s", "Agent", "Live", "Heur", "BPE", "L/H", "L/BPE")
		t.Logf("  %-24s  %8s  %8s  %8s  %8s  %8s", "------------------------", "--------", "--------", "--------", "--------", "--------")
	} else {
		t.Logf("  %-24s  %8s  %8s  %8s", "Agent", "Live", "Heur", "L/H")
		t.Logf("  %-24s  %8s  %8s  %8s", "------------------------", "--------", "--------", "--------")
	}

	for _, agentDef := range agents {
		toolDefs, err := builtins.ForNames(agentDef.Tools)
		if err != nil {
			t.Fatalf("resolving tools for %s: %v", agentDef.Name, err)
		}
		anthropicTools := benchmarks.ToolsToAnthropic(toolDefs)

		liveTokens, err := client.CountTokens(ctx, benchmarks.CountTokensRequest{
			Messages: []benchmarks.AnthropicMsg{{Role: "user", Content: "x"}},
			System:   agentDef.PromptBody,
			Tools:    anthropicTools,
		})
		if err != nil {
			t.Fatalf("count_tokens for %s failed: %v", agentDef.Name, err)
		}

		m := benchmarks.MeasureTools(toolDefs)
		heuristicTokens := m.SchemaTokens + benchmarks.EstimateTokens(agentDef.PromptBody)
		hRatio := float64(liveTokens) / float64(heuristicTokens)
		heuristicRatios = append(heuristicRatios, hRatio)

		if bpeErr == nil {
			bpeM := benchmarks.MeasureToolsWith(toolDefs, bpe)
			bpeTokens := bpeM.SchemaTokens + bpe.Count(agentDef.PromptBody)
			bRatio := float64(liveTokens) / float64(bpeTokens)
			bpeRatios = append(bpeRatios, bRatio)
			t.Logf("  %-24s  %8d  %8d  %8d  %8.2f  %8.2f",
				agentDef.Name, liveTokens, heuristicTokens, bpeTokens, hRatio, bRatio)
		} else {
			t.Logf("  %-24s  %8d  %8d  %8.2f",
				agentDef.Name, liveTokens, heuristicTokens, hRatio)
		}
	}

	// Compute averages.
	avgH := avg(heuristicRatios)
	t.Logf("")
	if avgH > 1.0 {
		t.Logf("  Heuristic underestimates by ~%.0f%% on average (avg live/heuristic = %.2f)",
			(avgH-1.0)*100, avgH)
	} else {
		t.Logf("  Heuristic overestimates by ~%.0f%% on average (avg live/heuristic = %.2f)",
			(1.0-avgH)*100, avgH)
	}

	if len(bpeRatios) > 0 {
		avgB := avg(bpeRatios)
		if avgB > 1.0 {
			t.Logf("  BPE underestimates by ~%.0f%% on average (avg live/BPE = %.2f)",
				(avgB-1.0)*100, avgB)
		} else {
			t.Logf("  BPE overestimates by ~%.0f%% on average (avg live/BPE = %.2f)",
				(1.0-avgB)*100, avgB)
		}
	}
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
