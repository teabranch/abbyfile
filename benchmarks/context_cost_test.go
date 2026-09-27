package benchmarks_test

import (
	"testing"

	"github.com/teabranch/abbyfile/benchmarks"
	"github.com/teabranch/abbyfile/pkg/builtins"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// representativeConfig is the agent Phase D measures: all builtins, memory
// enabled, and the benchmark system prompt (testdata/system.md, ~3.4 KB).
func representativeConfig(t *testing.T, eager bool) agentmcp.BridgeConfig {
	t.Helper()
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatalf("creating file store: %v", err)
	}
	mgr := memory.NewManager(store)

	registry := tools.NewRegistry()
	for _, def := range append(builtins.All(), mgr.Tools()...) {
		if err := registry.Register(def); err != nil {
			t.Fatalf("register %s: %v", def.Name, err)
		}
	}
	cfg := bridgeConfig(t, registry, mgr)
	cfg.Description = "Benchmark agent"
	cfg.EagerInstructions = eager
	return cfg
}

// TestHandshakeContextCost reports the per-session context cost (tools/list
// schemas + handshake instructions) in both eager modes. Numbers are
// recorded in docs/guides/benchmarks.md ("Per-session handshake cost").
func TestHandshakeContextCost(t *testing.T) {
	results := map[bool]*benchmarks.HandshakePayload{}
	for _, eager := range []bool{false, true} {
		p, err := benchmarks.MeasureHandshake(representativeConfig(t, eager))
		if err != nil {
			t.Fatalf("eager=%t: %v", eager, err)
		}
		results[eager] = p
		names := make([]string, len(p.Tools))
		for i, tm := range p.Tools {
			names[i] = tm.Name
		}
		t.Logf("| eager_instructions=%t | %d tools | tools/list ~%d tokens | instructions ~%d tokens | total ~%d tokens | %v",
			eager, len(p.Tools), p.TotalSchemaTokens, p.InstructionsTokens, p.HandshakeTokens, names)
	}

	stub, eager := results[false], results[true]
	if stub.InstructionsTokens == 0 || eager.InstructionsTokens == 0 {
		t.Fatalf("instructions not measured: stub=%d eager=%d", stub.InstructionsTokens, eager.InstructionsTokens)
	}
	if eager.InstructionsTokens <= stub.InstructionsTokens {
		t.Errorf("eager instructions (%d tokens) should exceed the stub (%d tokens)",
			eager.InstructionsTokens, stub.InstructionsTokens)
	}
	if stub.HandshakeTokens != stub.TotalSchemaTokens+stub.InstructionsTokens {
		t.Errorf("HandshakeTokens = %d, want schema+instructions = %d",
			stub.HandshakeTokens, stub.TotalSchemaTokens+stub.InstructionsTokens)
	}
}
