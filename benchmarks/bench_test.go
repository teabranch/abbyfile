package benchmarks_test

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentmcp "github.com/teabranch/agentfile/pkg/mcp"
	"github.com/teabranch/agentfile/pkg/tools"

	"github.com/teabranch/agentfile/benchmarks"
	"github.com/teabranch/agentfile/pkg/builtins"
	"github.com/teabranch/agentfile/pkg/definition"
	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/prompt"
)

//go:embed testdata/system.md
var testPromptFS embed.FS

func newTestLoader(t *testing.T) *prompt.Loader {
	t.Helper()
	return prompt.NewLoader("bench-agent", testPromptFS, "testdata/system.md")
}

// bridgeConfig creates a BridgeConfig with the given registry and optional memory.
func bridgeConfig(t *testing.T, registry *tools.Registry, mem *memory.Manager) agentmcp.BridgeConfig {
	t.Helper()
	return agentmcp.BridgeConfig{
		Name:     "bench-agent",
		Version:  "v0.1.0",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   newTestLoader(t),
		Memory:   mem,
	}
}

// --- Benchmarks: Handshake at varying tool counts ---

func BenchmarkHandshake_6Tools(b *testing.B) {
	benchmarkHandshakeN(b, 6)
}

func BenchmarkHandshake_10Tools(b *testing.B) {
	benchmarkHandshakeN(b, 10)
}

func BenchmarkHandshake_15Tools(b *testing.B) {
	benchmarkHandshakeN(b, 15)
}

func BenchmarkHandshake_20Tools(b *testing.B) {
	benchmarkHandshakeN(b, 20)
}

func BenchmarkHandshake_30Tools(b *testing.B) {
	benchmarkHandshakeN(b, 30)
}

func BenchmarkHandshake_50Tools(b *testing.B) {
	benchmarkHandshakeN(b, 50)
}

func BenchmarkHandshake_93Tools(b *testing.B) {
	benchmarkHandshakeN(b, 93)
}

func benchmarkHandshakeN(b *testing.B, n int) {
	b.Helper()
	defs := benchmarks.GenerateTools(n)
	registry := tools.NewRegistry()
	for _, def := range defs {
		if err := registry.Register(def); err != nil {
			b.Fatal(err)
		}
	}

	loader := prompt.NewLoader("bench-agent", testPromptFS, "testdata/system.md")
	cfg := agentmcp.BridgeConfig{
		Name:     "bench-agent",
		Version:  "v0.1.0",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   loader,
	}

	b.ResetTimer()
	for range b.N {
		payload, err := benchmarks.MeasureHandshake(cfg)
		if err != nil {
			b.Fatal(err)
		}
		// Report custom metrics.
		b.ReportMetric(float64(payload.TotalSchemaTokens), "schema_tokens")
		b.ReportMetric(float64(payload.TotalTokens), "total_tokens")
	}
}

// --- Test: Scaling Curve ---

func TestScalingCurve(t *testing.T) {
	sizes := benchmarks.DefaultScalingSizes()
	points := benchmarks.MeasureScalingCurve(sizes)

	t.Log("Scaling curve (synthetic tools):")
	for _, p := range points {
		label := ""
		if p.ToolCount == 15 {
			label = "  <- recommended max"
		} else if p.ToolCount == 50 {
			label = "  <- anti-pattern"
		}
		t.Logf("  %2d tools: %6d tokens  (%4.1f%% of 128K)%s",
			p.ToolCount, p.SchemaTokens, p.BudgetPct, label)
	}

	// Verify linear growth: doubling tools should roughly double tokens.
	if len(points) >= 2 {
		tokensPerTool := make([]float64, len(points))
		for i, p := range points {
			tokensPerTool[i] = float64(p.SchemaTokens) / float64(p.ToolCount)
		}
		// Per-tool cost should be roughly constant (within 20% variance).
		avg := tokensPerTool[0]
		for _, tpt := range tokensPerTool[1:] {
			ratio := tpt / avg
			if ratio < 0.8 || ratio > 1.2 {
				t.Errorf("non-linear scaling: per-tool cost varies too much (%.1f vs %.1f avg)", tpt, avg)
			}
		}
	}
}

// --- Test: Community Agents ---

func TestCommunityAgents(t *testing.T) {
	communityDir := filepath.Join("testdata", "community")
	entries, err := os.ReadDir(communityDir)
	if err != nil {
		t.Fatalf("reading community dir: %v", err)
	}

	var agents []benchmarks.AgentMeasurement
	t.Log("Community agent baselines (awesome-claude-code-subagents):")

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		def, err := definition.ParseAgentMD(path)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}

		// Measure tools.
		toolDefs, err := builtins.ForNames(def.Tools)
		if err != nil {
			t.Fatalf("resolving tools for %s: %v", def.Name, err)
		}
		m := benchmarks.MeasureTools(toolDefs)

		// Measure prompt.
		promptTokens := benchmarks.EstimateTokens(def.PromptBody)
		totalTokens := m.SchemaTokens + promptTokens
		budgetPct := float64(totalTokens) / float64(benchmarks.ContextWindow) * 100

		am := benchmarks.AgentMeasurement{
			Name:         def.Name,
			ToolCount:    m.ToolCount,
			SchemaBytes:  m.SchemaBytes,
			SchemaTokens: m.SchemaTokens,
			PromptBytes:  len(def.PromptBody),
			PromptTokens: promptTokens,
			TotalTokens:  totalTokens,
			BudgetPct:    budgetPct,
		}
		agents = append(agents, am)

		t.Logf("  %-24s (%d tools): ~%d schema + ~%d prompt = ~%d total (%.1f%% of 128K)",
			def.Name, m.ToolCount, m.SchemaTokens, promptTokens, totalTokens, budgetPct)
	}

	if len(agents) != 5 {
		t.Errorf("expected 5 community agents, got %d", len(agents))
	}

	// Verify all are under 5% individually.
	for _, a := range agents {
		if a.BudgetPct > 5.0 {
			t.Errorf("agent %s exceeds 5%% budget: %.1f%%", a.Name, a.BudgetPct)
		}
	}

	// Combined overhead.
	combinedTokens := 0
	for _, a := range agents {
		combinedTokens += a.TotalTokens
	}
	combinedPct := float64(combinedTokens) / float64(benchmarks.ContextWindow) * 100
	t.Logf("\n  Combined (%d agents): ~%d tokens (%.1f%% of 128K)", len(agents), combinedTokens, combinedPct)
}

// --- Test: Redundancy Audit ---

func TestRedundancyAudit(t *testing.T) {
	// Measure the three channels through which prompt/instructions travel:
	// 1. ServerOptions.Instructions (set during MCP handshake)
	// 2. get_instructions tool definition (tool schema overhead)
	// 3. system prompt template (GetPrompt result)

	registry := tools.NewRegistry()
	cfg := bridgeConfig(t, registry, nil)

	payload, err := benchmarks.MeasureHandshake(cfg)
	if err != nil {
		t.Fatalf("measuring handshake: %v", err)
	}

	// Find get_instructions tool overhead.
	var getInstrTokens int
	for _, tool := range payload.Tools {
		if tool.Name == "get_instructions" {
			getInstrTokens = tool.SchemaTokens
			break
		}
	}

	t.Log("Redundancy audit (prompt delivery channels):")
	t.Logf("  ServerOptions.Instructions (handshake): ~%d tokens (delivered once)", payload.PromptTokens)
	t.Logf("  get_instructions tool (schema overhead): ~%d tokens (per ListTools)", getInstrTokens)
	t.Logf("  system prompt template (GetPrompt):      ~%d tokens (on-demand)", payload.PromptTokens)
	t.Logf("  Note: get_instructions is kept for backward compatibility; real cost is schema only")
}

// --- Test: Comparison With Article ---

func TestComparisonWithArticle(t *testing.T) {
	// Build full report.
	report := &benchmarks.BenchmarkReport{}

	// Community agents.
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
		toolDefs, err := builtins.ForNames(def.Tools)
		if err != nil {
			t.Fatalf("resolving tools for %s: %v", def.Name, err)
		}
		m := benchmarks.MeasureTools(toolDefs)
		promptTokens := benchmarks.EstimateTokens(def.PromptBody)
		totalTokens := m.SchemaTokens + promptTokens
		report.CommunityAgents = append(report.CommunityAgents, benchmarks.AgentMeasurement{
			Name:         def.Name,
			ToolCount:    m.ToolCount,
			SchemaBytes:  m.SchemaBytes,
			SchemaTokens: m.SchemaTokens,
			PromptBytes:  len(def.PromptBody),
			PromptTokens: promptTokens,
			TotalTokens:  totalTokens,
			BudgetPct:    float64(totalTokens) / float64(benchmarks.ContextWindow) * 100,
		})
	}

	// Scaling curve.
	report.ScalingCurve = benchmarks.MeasureScalingCurve(benchmarks.DefaultScalingSizes())

	// Output full report.
	t.Log("\n" + report.Text())

	// Verify key claims.
	// 1. A 10-tool agent should be at least 20x smaller than article's 55K.
	for _, p := range report.ScalingCurve {
		if p.ToolCount == 10 {
			ratio := float64(benchmarks.ArticleReference.GitHubMCP93Tools) / float64(p.SchemaTokens)
			if ratio < 20 {
				t.Errorf("expected at least 20x reduction for 10 tools, got %.1fx", ratio)
			}
			t.Logf("10-tool agent: %.0fx smaller than GitHub MCP (93 tools)", ratio)
		}
	}

	// 2. All 5 community agents combined should be well under enterprise stack.
	combinedCommunity := 0
	for _, a := range report.CommunityAgents {
		combinedCommunity += a.TotalTokens
	}
	ratio := float64(benchmarks.ArticleReference.EnterpriseMCPStack) / float64(combinedCommunity)
	if ratio < 5 {
		t.Errorf("expected at least 5x reduction vs enterprise stack, got %.1fx", ratio)
	}
	t.Logf("5 community agents combined: %.0fx smaller than enterprise MCP stack", ratio)

	// JSON output for machine consumption.
	jsonStr, err := report.JSON()
	if err != nil {
		t.Fatalf("marshaling report JSON: %v", err)
	}
	// Validate it parses back.
	var check benchmarks.BenchmarkReport
	if err := json.Unmarshal([]byte(jsonStr), &check); err != nil {
		t.Fatalf("report JSON round-trip failed: %v", err)
	}
}

// --- Test: Anti-Pattern Threshold ---

func TestAntiPatternThreshold(t *testing.T) {
	sizes := benchmarks.DefaultScalingSizes()
	points := benchmarks.MeasureScalingCurve(sizes)

	t.Log(benchmarks.FormatAntiPatternAnalysis(points))

	// Verify: 50+ tools should exceed some reasonable threshold.
	// With realistic schemas, 50 tools should use significant budget.
	for _, p := range points {
		if p.ToolCount >= 50 {
			if p.BudgetPct < 3.0 {
				t.Errorf("expected 50+ tools to exceed 3%% budget, got %.1f%%", p.BudgetPct)
			}
			t.Logf("50+ tools at %.1f%% of context budget - confirms anti-pattern at scale", p.BudgetPct)
			break
		}
	}

	// Verify: under 15 tools should stay under 5%.
	for _, p := range points {
		if p.ToolCount <= 15 && p.BudgetPct > 5.0 {
			t.Errorf("%d tools exceeds 5%% budget: %.1f%%", p.ToolCount, p.BudgetPct)
		}
	}
}

// --- Test: MeasureRegistry with real builtins ---

func TestMeasureRegistryBuiltins(t *testing.T) {
	registry := tools.NewRegistry()
	for _, def := range builtins.All() {
		if err := registry.Register(def); err != nil {
			t.Fatal(err)
		}
	}

	m := benchmarks.MeasureRegistry(registry)

	t.Logf("Builtin tools (%d): %d bytes, ~%d tokens", m.ToolCount, m.SchemaBytes, m.SchemaTokens)
	for _, tm := range m.PerTool {
		t.Logf("  %-15s %4d bytes  ~%3d tokens", tm.Name, tm.SchemaBytes, tm.SchemaTokens)
	}

	if m.ToolCount != 6 {
		t.Errorf("expected 6 builtin tools, got %d", m.ToolCount)
	}
	if m.SchemaBytes == 0 {
		t.Error("schema bytes should be non-zero")
	}
}

// --- Test: MeasureHandshake with memory ---

func TestMeasureHandshakeWithMemory(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatalf("creating file store: %v", err)
	}
	mgr := memory.NewManager(store)

	registry := tools.NewRegistry()
	for _, def := range mgr.Tools() {
		if err := registry.Register(def); err != nil {
			t.Fatal(err)
		}
	}

	cfg := bridgeConfig(t, registry, mgr)
	payload, err := benchmarks.MeasureHandshake(cfg)
	if err != nil {
		t.Fatalf("measuring handshake: %v", err)
	}

	// Should include memory tools + get_instructions.
	t.Logf("Handshake with memory: %d tools, ~%d schema tokens, ~%d prompt tokens, ~%d total",
		len(payload.Tools), payload.TotalSchemaTokens, payload.PromptTokens, payload.TotalTokens)

	// Memory adds 4 tools.
	memToolCount := 0
	for _, tool := range payload.Tools {
		if strings.HasPrefix(tool.Name, "memory_") {
			memToolCount++
		}
	}
	if memToolCount != 5 {
		t.Errorf("expected 5 memory tools, got %d", memToolCount)
	}

	fmt.Fprintf(os.Stderr, "Context budget: %.1f%% of 128K\n", payload.ContextBudgetPercent())
}

// --- Test: EstimateTokens ---

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"hello world", 3}, // 11 bytes -> (11+3)/4 = 3
	}

	for _, tt := range tests {
		got := benchmarks.EstimateTokens(tt.input)
		if got != tt.expected {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// --- Test: GenerateTools ---

func TestGenerateTools(t *testing.T) {
	for _, n := range []int{1, 6, 10, 50, 93} {
		defs := benchmarks.GenerateTools(n)
		if len(defs) != n {
			t.Errorf("GenerateTools(%d) returned %d tools", n, len(defs))
		}

		// All tools should have unique names.
		names := make(map[string]bool)
		for _, def := range defs {
			if names[def.Name] {
				t.Errorf("duplicate tool name: %s", def.Name)
			}
			names[def.Name] = true
		}
	}
}

// ==========================================================================
// New rigorous benchmarks: tokenizer, multi-turn, baseline, article
// ==========================================================================

// --- Test: Tokenizer Comparison (bytes/4 vs real BPE) ---

func TestTokenizerComparison(t *testing.T) {
	bpe, err := benchmarks.NewBPECounter()
	if err != nil {
		t.Skipf("BPE tokenizer not available (network required on first run): %v", err)
	}

	t.Log("Tokenizer comparison: bytes/4 heuristic vs cl100k_base BPE\n")

	// 1. Measure all 6 builtins.
	t.Log("  Built-in tool schemas:")
	allBuiltins := builtins.All()
	for _, def := range allBuiltins {
		data, _ := json.Marshal(struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		}{def.Name, def.Description, def.InputSchema})
		cmp := benchmarks.CompareTokenizers(def.Name, string(data), bpe)
		t.Logf("    %-15s %4d bytes  heuristic:%3d  BPE:%3d  ratio:%.2f  bytes/BPE:%.1f",
			cmp.Label, cmp.ByteCount, cmp.Heuristic, cmp.BPE, cmp.Ratio, cmp.BytesPerBP)
	}

	// 2. Measure synthetic tools at varying counts.
	t.Log("\n  Synthetic focused tools (aggregate):")
	for _, n := range benchmarks.DefaultScalingSizes() {
		defs := benchmarks.GenerateTools(n)
		heur := benchmarks.MeasureToolsWith(defs, benchmarks.BytesEstimator{})
		bpeM := benchmarks.MeasureToolsWith(defs, bpe)
		ratio := float64(bpeM.SchemaTokens) / float64(heur.SchemaTokens)
		t.Logf("    %2d tools:  heuristic:%5d  BPE:%5d  ratio:%.2f",
			n, heur.SchemaTokens, bpeM.SchemaTokens, ratio)
	}

	// 3. Measure platform-style tools.
	t.Log("\n  Synthetic platform tools (GitHub MCP-style):")
	for _, n := range []int{10, 50, 93} {
		defs := benchmarks.GeneratePlatformTools(n)
		heur := benchmarks.MeasureToolsWith(defs, benchmarks.BytesEstimator{})
		bpeM := benchmarks.MeasureToolsWith(defs, bpe)
		ratio := float64(bpeM.SchemaTokens) / float64(heur.SchemaTokens)
		t.Logf("    %2d tools:  heuristic:%5d  BPE:%5d  ratio:%.2f",
			n, heur.SchemaTokens, bpeM.SchemaTokens, ratio)
	}

	// 4. Measure community agent prompts (natural language).
	t.Log("\n  Community agent prompts (natural language):")
	communityDir := filepath.Join("testdata", "community")
	entries, _ := os.ReadDir(communityDir)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		def, err := definition.ParseAgentMD(path)
		if err != nil {
			continue
		}
		cmp := benchmarks.CompareTokenizers(def.Name, def.PromptBody, bpe)
		t.Logf("    %-24s %5d bytes  heuristic:%4d  BPE:%4d  ratio:%.2f  bytes/BPE:%.1f",
			cmp.Label, cmp.ByteCount, cmp.Heuristic, cmp.BPE, cmp.Ratio, cmp.BytesPerBP)
	}

	// 5. Summary: what's the average correction factor?
	// Measure a representative sample and compute average ratio.
	var ratios []float64
	for _, def := range allBuiltins {
		data, _ := json.Marshal(struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"inputSchema"`
		}{def.Name, def.Description, def.InputSchema})
		cmp := benchmarks.CompareTokenizers(def.Name, string(data), bpe)
		ratios = append(ratios, cmp.Ratio)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		def, _ := definition.ParseAgentMD(path)
		if def == nil {
			continue
		}
		cmp := benchmarks.CompareTokenizers(def.Name, def.PromptBody, bpe)
		ratios = append(ratios, cmp.Ratio)
	}
	avgRatio := 0.0
	for _, r := range ratios {
		avgRatio += r
	}
	avgRatio /= float64(len(ratios))
	t.Logf("\n  Average BPE/heuristic ratio: %.2f", avgRatio)
	if avgRatio > 1.0 {
		t.Logf("  Interpretation: bytes/4 underestimates real BPE token count by ~%.0f%%",
			(avgRatio-1.0)*100)
	} else {
		t.Logf("  Interpretation: bytes/4 overestimates real BPE token count by ~%.0f%%",
			(1.0-avgRatio)*100)
	}
	t.Log("  Note: cl100k_base is a proxy for Claude's tokenizer, not exact")
}

// --- Test: Multi-Turn Projection ---

func TestMultiTurnProjection(t *testing.T) {
	turns := benchmarks.DefaultTurnCounts()

	// Agentfile 10-tool agent.
	focusedTools := benchmarks.MeasureTools(benchmarks.GenerateTools(10))
	agentfileReport := benchmarks.ProjectMultiTurn(
		"Agentfile (10 tools + prompt)",
		focusedTools.SchemaTokens,
		845, // prompt tokens from our system.md testdata
		turns,
	)

	// GitHub MCP (article's numbers).
	githubReport := benchmarks.ProjectMultiTurn(
		"GitHub MCP (93 tools, article)",
		benchmarks.ArticleReference.GitHubMCP93Tools,
		0, // article doesn't separate prompt from tools
		turns,
	)

	// Enterprise stack (article's numbers).
	enterpriseReport := benchmarks.ProjectMultiTurn(
		"Enterprise stack (article)",
		benchmarks.ArticleReference.EnterpriseMCPStack,
		0,
		turns,
	)

	t.Log("\n" + benchmarks.FormatMultiTurnComparison([]*benchmarks.MultiTurnReport{
		githubReport,
		agentfileReport,
	}))

	t.Log("\n" + benchmarks.FormatMultiTurnComparison([]*benchmarks.MultiTurnReport{
		enterpriseReport,
		agentfileReport,
	}))

	// Verify: over 20 turns, the savings are massive.
	for _, p := range githubReport.Projections {
		if p.Turns == 20 {
			githubCumulative := p.CumulativeTotal
			agentfileCumulative := agentfileReport.PerTurn * 20
			saved := githubCumulative - agentfileCumulative
			t.Logf("Over 20 turns: GitHub MCP = %dT, Agentfile = %dT, saved = %dT (%.0fx reduction)",
				githubCumulative, agentfileCumulative, saved,
				float64(githubCumulative)/float64(agentfileCumulative))
		}
	}
}

// --- Test: Claude Code Baseline ---

func TestClaudeCodeBaseline(t *testing.T) {
	baseline := benchmarks.EstimateClaudeCodeBaseline()

	// Measure real Agentfile builtin tools for marginal cost.
	allDefs := builtins.All()
	toolMeasure := benchmarks.MeasureTools(allDefs)
	promptTokens := benchmarks.EstimateTokens("You are a senior Go developer...") // representative prompt
	agentTokens := toolMeasure.SchemaTokens + 845                                 // using testdata prompt size

	marginal := benchmarks.ComputeMarginalCost("10-tool agent", agentTokens, baseline)

	// Also compute for 5 community agents loaded simultaneously.
	communityDir := filepath.Join("testdata", "community")
	entries, _ := os.ReadDir(communityDir)
	communityTotal := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		def, err := definition.ParseAgentMD(path)
		if err != nil {
			continue
		}
		toolDefs, _ := builtins.ForNames(def.Tools)
		m := benchmarks.MeasureTools(toolDefs)
		communityTotal += m.SchemaTokens + benchmarks.EstimateTokens(def.PromptBody)
	}
	marginal5 := benchmarks.ComputeMarginalCost("5 community agents", communityTotal, baseline)

	// GitHub MCP for contrast.
	githubMarginal := benchmarks.ComputeMarginalCost("GitHub MCP (93 tools)", benchmarks.ArticleReference.GitHubMCP93Tools, baseline)

	_ = promptTokens // used above in agentTokens calculation

	t.Log("\n" + benchmarks.FormatBaselineAnalysis(baseline, []*benchmarks.MarginalCost{
		marginal,
		marginal5,
		githubMarginal,
	}))

	// Verify: agent marginal cost should be small relative to baseline.
	if marginal.MarginalPercent > 50 {
		t.Errorf("10-tool agent adds %.1f%% over baseline, expected < 50%%", marginal.MarginalPercent)
	}
	t.Logf("10-tool agent adds %.1f%% over Claude Code baseline (%.1f -> %.1f%% of 128K)",
		marginal.MarginalPercent, marginal.BudgetBefore, marginal.BudgetAfter)

	// Verify: GitHub MCP marginal cost is huge.
	if githubMarginal.MarginalPercent < 200 {
		t.Errorf("GitHub MCP should add >200%% over baseline, got %.1f%%", githubMarginal.MarginalPercent)
	}
	t.Logf("GitHub MCP adds %.1f%% over Claude Code baseline (%.1f -> %.1f%% of 128K)",
		githubMarginal.MarginalPercent, githubMarginal.BudgetBefore, githubMarginal.BudgetAfter)
}

// --- Test: Article Methodology ---

func TestArticleMethodology(t *testing.T) {
	// Test with bytes/4 heuristic.
	t.Log("\n" + benchmarks.FormatArticleComparison(benchmarks.BytesEstimator{}))

	// Test with BPE if available.
	bpe, err := benchmarks.NewBPECounter()
	if err != nil {
		t.Logf("Skipping BPE comparison (network required on first run): %v", err)
		return
	}
	t.Log("\n" + benchmarks.FormatArticleComparison(bpe))

	// Verify: platform tools should be significantly larger per-tool than focused tools.
	cmp := benchmarks.CompareToolComplexity(93, benchmarks.BytesEstimator{})
	if cmp.ComplexityRatio < 3.0 {
		t.Errorf("expected platform tools to be >3x larger per-tool, got %.1fx", cmp.ComplexityRatio)
	}
	t.Logf("Platform tools are %.1fx larger per-tool than focused tools", cmp.ComplexityRatio)

	// Verify: our platform-style 93 tools should be in the same order of magnitude
	// as the article's 55K (accounting for the fact that the real GitHub MCP has
	// even more complex schemas with nested objects, enums, etc.).
	if cmp.PlatformTotal < 5000 {
		t.Errorf("expected platform 93-tool total > 5000 tokens, got %d", cmp.PlatformTotal)
	}
	t.Logf("Our platform-style 93 tools: %d tokens (article: %d tokens, our focused 10: %d tokens)",
		cmp.PlatformTotal, benchmarks.ArticleReference.GitHubMCP93Tools,
		benchmarks.MeasureTools(benchmarks.GenerateTools(10)).SchemaTokens)
}

// --- Test: GeneratePlatformTools ---

// ==========================================================================
// Skills vs Sub-agents vs Agentfile benchmarks
// ==========================================================================

// --- Test: Skills Comparison ---

func TestSkillsComparison(t *testing.T) {
	// Measure a real community agent.
	communityDir := filepath.Join("testdata", "community")
	entries, err := os.ReadDir(communityDir)
	if err != nil {
		t.Fatalf("reading community dir: %v", err)
	}

	// Use the first community agent as representative.
	var agentDef *definition.AgentDef
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		agentDef, err = definition.ParseAgentMD(path)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		break
	}
	if agentDef == nil {
		t.Fatal("no community agent found")
	}

	// Measure the agent's actual cost.
	toolDefs, err := builtins.ForNames(agentDef.Tools)
	if err != nil {
		t.Fatalf("resolving tools for %s: %v", agentDef.Name, err)
	}
	m := benchmarks.MeasureTools(toolDefs)
	promptTokens := benchmarks.EstimateTokens(agentDef.PromptBody)

	agentfileCost := benchmarks.AgentfileCost{
		Name:            agentDef.Name,
		ToolTokens:      m.SchemaTokens,
		PromptTokens:    promptTokens,
		TotalTokens:     m.SchemaTokens + promptTokens,
		HasTools:        true,
		HasMemory:       true,
		HasVersioning:   true,
		HasDistribution: true,
	}

	skillCost := benchmarks.EstimateSkillCost("equivalent-skill", benchmarks.DefaultSkillTiers())
	cmp := benchmarks.CompareSkillsVsAgentfile(skillCost, &agentfileCost)

	t.Log("\n" + benchmarks.FormatSkillsComparison(cmp))

	// Verify: Agentfile token cost is in the same order of magnitude as loaded skill (within 3x).
	if cmp.TokenRatio > 3.0 {
		t.Errorf("Agentfile costs %.1fx a loaded skill — expected within 3x", cmp.TokenRatio)
	}
	t.Logf("Agentfile costs %.1fx a loaded skill (same order of magnitude)", cmp.TokenRatio)

	// Verify: Agentfile has capabilities skills lack.
	if len(cmp.FeatureDelta) < 3 {
		t.Errorf("expected at least 3 feature advantages, got %d", len(cmp.FeatureDelta))
	}
	t.Logf("Agentfile adds %d capabilities over skills: %v", len(cmp.FeatureDelta), cmp.FeatureDelta)
}

// --- Test: Sub-Agent Cost Model ---

func TestSubAgentCostModel(t *testing.T) {
	// Build a representative 10-tool agent.
	focusedTools := benchmarks.MeasureTools(benchmarks.GenerateTools(10))
	promptTokens := 845 // from testdata prompt size

	inv := benchmarks.EstimateSubAgentInvocation(focusedTools.SchemaTokens, promptTokens)
	t.Log("\n" + benchmarks.FormatSubAgentBreakdown(inv))

	// Project over standard invocation counts.
	counts := benchmarks.DefaultInvocationCounts()
	agentfilePerTurn := focusedTools.SchemaTokens + promptTokens

	cmp := benchmarks.CompareSubAgentVsAgentfile(inv, agentfilePerTurn, counts)
	t.Log("\n" + benchmarks.FormatSubAgentComparison(cmp))

	// Verify: sub-agent cumulative exceeds Agentfile by >3x at 5+ invocations.
	for _, p := range cmp.Projections {
		if p.Invocations >= 5 && p.Ratio < 3.0 {
			t.Errorf("at %d invocations, sub-agent should cost >3x Agentfile, got %.1fx",
				p.Invocations, p.Ratio)
		}
	}

	// Verify: the ratio is consistent (since both scale linearly).
	if len(cmp.Projections) >= 2 {
		r1 := cmp.Projections[0].Ratio
		r2 := cmp.Projections[len(cmp.Projections)-1].Ratio
		if r1 > 0 && r2 > 0 {
			diff := r1 - r2
			if diff < 0 {
				diff = -diff
			}
			if diff > 0.1 {
				t.Errorf("ratio should be constant across invocations, got %.1f and %.1f", r1, r2)
			}
		}
	}

	t.Logf("Sub-agent costs %.1fx Agentfile per invocation (%dT vs %dT)",
		cmp.Projections[0].Ratio, inv.PerCallTokens, agentfilePerTurn)
}

// --- Test: Three-Way Comparison ---

func TestThreeWayComparison(t *testing.T) {
	// Use a real community agent for measured data.
	communityDir := filepath.Join("testdata", "community")
	entries, err := os.ReadDir(communityDir)
	if err != nil {
		t.Fatalf("reading community dir: %v", err)
	}

	var agentDef *definition.AgentDef
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(communityDir, entry.Name())
		agentDef, err = definition.ParseAgentMD(path)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}
		break
	}
	if agentDef == nil {
		t.Fatal("no community agent found")
	}

	toolDefs, err := builtins.ForNames(agentDef.Tools)
	if err != nil {
		t.Fatalf("resolving tools for %s: %v", agentDef.Name, err)
	}
	m := benchmarks.MeasureTools(toolDefs)
	promptTokens := benchmarks.EstimateTokens(agentDef.PromptBody)

	cmp := benchmarks.CompareThreeWay(m.SchemaTokens, promptTokens)
	t.Log("\n" + benchmarks.FormatThreeWayComparison(cmp))
	t.Log("\n" + benchmarks.FormatThreeWaySummary())

	// Verify: Agentfile per-turn cost < sub-agent per-call cost.
	if cmp.Agentfile.PerTurnCost >= cmp.SubAgent.PerTurnCost {
		t.Errorf("Agentfile per-turn (%d) should be less than sub-agent per-call (%d)",
			cmp.Agentfile.PerTurnCost, cmp.SubAgent.PerTurnCost)
	}

	// Verify: Agentfile has more capabilities than skills.
	if len(cmp.Agentfile.Capabilities) <= len(cmp.Skill.Capabilities) {
		t.Errorf("Agentfile should have more capabilities (%d) than skills (%d)",
			len(cmp.Agentfile.Capabilities), len(cmp.Skill.Capabilities))
	}

	// Verify: Agentfile per-turn cost is in same order of magnitude as loaded skill.
	ratio := float64(cmp.Agentfile.PerTurnCost) / float64(cmp.Skill.PerTurnCost)
	if ratio > 3.0 {
		t.Errorf("Agentfile per-turn (%d) should be within 3x of skill loaded (%d), got %.1fx",
			cmp.Agentfile.PerTurnCost, cmp.Skill.PerTurnCost, ratio)
	}
	t.Logf("Cost ratio: Agentfile/Skill=%.1fx, SubAgent/Agentfile=%.1fx",
		ratio, float64(cmp.SubAgent.PerTurnCost)/float64(cmp.Agentfile.PerTurnCost))
}

func TestGeneratePlatformTools(t *testing.T) {
	for _, n := range []int{1, 10, 50, 93} {
		defs := benchmarks.GeneratePlatformTools(n)
		if len(defs) != n {
			t.Errorf("GeneratePlatformTools(%d) returned %d tools", n, len(defs))
		}
		names := make(map[string]bool)
		for _, def := range defs {
			if names[def.Name] {
				t.Errorf("duplicate platform tool name: %s", def.Name)
			}
			names[def.Name] = true
			// Platform tools should have longer descriptions.
			if len(def.Description) < 100 {
				t.Errorf("platform tool %s has short description (%d chars)", def.Name, len(def.Description))
			}
		}
	}
}
