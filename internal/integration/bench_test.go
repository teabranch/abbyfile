//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teabranch/abbyfile/benchmarks"
	"github.com/teabranch/abbyfile/pkg/definition"
)

// --- Startup latency benchmarks ---

func BenchmarkStartup_Version(b *testing.B) {
	for range b.N {
		cmd := exec.Command(binaryPath, "--version")
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStartup_Describe(b *testing.B) {
	for range b.N {
		cmd := exec.Command(binaryPath, "--describe")
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStartup_CustomInstructions(b *testing.B) {
	for range b.N {
		cmd := exec.Command(binaryPath, "--custom-instructions")
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStartup_ServeMCP(b *testing.B) {
	for range b.N {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
		client := gomcp.NewClient(&gomcp.Implementation{
			Name:    "bench-client",
			Version: "v0.1.0",
		}, nil)

		session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			cancel()
			b.Fatal(err)
		}
		session.Close()
		cancel()
	}
}

// --- Per-call cost benchmarks ---

func BenchmarkToolCall_Builtin(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "bench-client",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()

	// Create a temp file to read.
	tmpFile := filepath.Join(b.TempDir(), "bench.txt")
	os.WriteFile(tmpFile, []byte("benchmark content"), 0o644)

	b.ResetTimer()
	for range b.N {
		_, err := session.CallTool(ctx, &gomcp.CallToolParams{
			Name:      "read_file",
			Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q}`, tmpFile)),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToolCall_Custom(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "bench-client",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()

	b.ResetTimer()
	for range b.N {
		_, err := session.CallTool(ctx, &gomcp.CallToolParams{
			Name:      "echo_stdin",
			Arguments: json.RawMessage(`{"message":"bench"}`),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToolCall_Memory(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tmpHome := b.TempDir()
	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "bench-client",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer session.Close()

	// Write a key first.
	_, err = session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "memory_write",
		Arguments: json.RawMessage(`{"key":"bench-key","value":"bench-value"}`),
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_, err := session.CallTool(ctx, &gomcp.CallToolParams{
			Name:      "memory_read",
			Arguments: json.RawMessage(`{"key":"bench-key"}`),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// --- Real binary validation ---

func TestMeasureRealHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "serve-mcp")
	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "bench-client",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer session.Close()

	// List tools and measure.
	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	totalBytes := 0
	t.Log("Real binary handshake tool measurements:")
	for _, tool := range listResult.Tools {
		data, _ := json.Marshal(tool)
		tokens := benchmarks.EstimateTokens(string(data))
		t.Logf("  %-20s %4d bytes  ~%3d tokens", tool.Name, len(data), tokens)
		totalBytes += len(data)
	}

	totalTokens := benchmarks.EstimateTokens(string(make([]byte, totalBytes)))
	budgetPct := float64(totalTokens) / float64(benchmarks.ContextWindow) * 100
	t.Logf("\n  Total: %d tools, %d bytes, ~%d tokens (%.1f%% of 128K)",
		len(listResult.Tools), totalBytes, totalTokens, budgetPct)

	// Get prompt for full cost.
	promptResult, err := session.GetPrompt(ctx, &gomcp.GetPromptParams{Name: "system"})
	if err != nil {
		t.Fatalf("getting system prompt: %v", err)
	}
	if len(promptResult.Messages) > 0 {
		if tc, ok := promptResult.Messages[0].Content.(*gomcp.TextContent); ok {
			promptTokens := benchmarks.EstimateTokens(tc.Text)
			t.Logf("  System prompt: %d bytes, ~%d tokens", len(tc.Text), promptTokens)
			t.Logf("  Total with prompt: ~%d tokens (%.1f%% of 128K)",
				totalTokens+promptTokens,
				float64(totalTokens+promptTokens)/float64(benchmarks.ContextWindow)*100)
		}
	}
}

// --- Auto-discovery of all agents ---

func TestMeasureAllAgents(t *testing.T) {
	projectRoot := findProjectRoot()

	// Find Abbyfile.
	abbyfilePath := filepath.Join(projectRoot, "Abbyfile")
	if _, err := os.Stat(abbyfilePath); os.IsNotExist(err) {
		abbyfilePath = filepath.Join(projectRoot, "abbyfile.yaml")
	}
	if _, err := os.Stat(abbyfilePath); os.IsNotExist(err) {
		t.Skip("no Abbyfile found at project root")
	}

	af, err := definition.ParseAbbyfile(abbyfilePath)
	if err != nil {
		t.Fatalf("parsing Abbyfile: %v", err)
	}

	// Build each agent and measure.
	tmp := t.TempDir()
	buildDir := filepath.Join(tmp, "build")
	os.MkdirAll(buildDir, 0o755)

	var agents []benchmarks.AgentMeasurement
	combinedTokens := 0

	for name, ref := range af.Agents {
		// Parse agent definition for tool/prompt info.
		agentMDPath := filepath.Join(projectRoot, ref.Path)
		if _, err := os.Stat(agentMDPath); os.IsNotExist(err) {
			t.Logf("Skipping %s: agent .md file not found at %s", name, ref.Path)
			continue
		}

		def, err := definition.ParseAgentMD(agentMDPath)
		if err != nil {
			t.Logf("Skipping %s: %v", name, err)
			continue
		}

		// Build the agent binary.
		buildCmd := exec.Command(abbyBin, "build",
			"-f", abbyfilePath,
			"-o", buildDir,
			"--agent", name,
		)
		buildCmd.Dir = projectRoot
		if out, err := buildCmd.CombinedOutput(); err != nil {
			t.Logf("Skipping %s: build failed: %v\n%s", name, err, out)
			continue
		}

		binPath := filepath.Join(buildDir, name)

		// Connect via MCP and measure.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, binPath, "serve-mcp")
		cmd.Env = append(os.Environ(), "HOME="+tmp)

		client := gomcp.NewClient(&gomcp.Implementation{
			Name:    "bench-client",
			Version: "v0.1.0",
		}, nil)

		session, err := client.Connect(ctx, &gomcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			cancel()
			t.Logf("Skipping %s: MCP connect failed: %v", name, err)
			continue
		}

		listResult, err := session.ListTools(ctx, nil)
		if err != nil {
			session.Close()
			cancel()
			t.Logf("Skipping %s: list tools failed: %v", name, err)
			continue
		}

		schemaBytes := 0
		for _, tool := range listResult.Tools {
			data, _ := json.Marshal(tool)
			schemaBytes += len(data)
		}
		schemaTokens := benchmarks.EstimateTokens(string(make([]byte, schemaBytes)))

		promptTokens := benchmarks.EstimateTokens(def.PromptBody)
		totalTokens := schemaTokens + promptTokens
		budgetPct := float64(totalTokens) / float64(benchmarks.ContextWindow) * 100

		am := benchmarks.AgentMeasurement{
			Name:         name,
			ToolCount:    len(listResult.Tools),
			SchemaBytes:  schemaBytes,
			SchemaTokens: schemaTokens,
			PromptBytes:  len(def.PromptBody),
			PromptTokens: promptTokens,
			TotalTokens:  totalTokens,
			BudgetPct:    budgetPct,
		}
		agents = append(agents, am)
		combinedTokens += totalTokens

		session.Close()
		cancel()
	}

	if len(agents) == 0 {
		t.Skip("no agents could be built and measured")
	}

	// Output results.
	t.Log("\n=== Abbyfile MCP Token Cost Analysis (Real Binaries) ===\n")
	t.Log("Per-agent measurements (auto-discovered from Abbyfile):")
	for _, a := range agents {
		t.Logf("  %-20s (%2d tools): ~%5d schema + ~%5d prompt = ~%5d total (%.1f%% of 128K)",
			a.Name, a.ToolCount, a.SchemaTokens, a.PromptTokens, a.TotalTokens, a.BudgetPct)
	}

	combinedPct := float64(combinedTokens) / float64(benchmarks.ContextWindow) * 100
	t.Logf("\nCombined (all %d agents loaded simultaneously):", len(agents))
	t.Logf("  %d agents: ~%d tokens (%.1f%% of 128K)", len(agents), combinedTokens, combinedPct)

	// Comparison.
	t.Logf("\nvs Article:")
	t.Logf("  GitHub MCP (93 tools): ~%d tokens (%.0f%% of 128K)",
		benchmarks.ArticleReference.GitHubMCP93Tools, benchmarks.ArticleReference.GitHubBudgetPercent)
	if combinedTokens > 0 {
		ratio := float64(benchmarks.ArticleReference.GitHubMCP93Tools) / float64(combinedTokens)
		t.Logf("  All Abbyfile agents:  ~%d tokens (%.1f%% of 128K) -> %.0fx smaller",
			combinedTokens, combinedPct, ratio)
	}
}
