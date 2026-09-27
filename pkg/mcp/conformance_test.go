package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func eraConfig(t *testing.T) agentmcp.BridgeConfig {
	return agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0", Description: "Era test agent",
		Registry: wireRegistry(), Executor: tools.NewExecutor(30*time.Second, nil),
		Loader: newTestLoader(t),
	}
}

func TestDualEra(t *testing.T) { // Review Focus #5 covers the 2025-06-18 row
	eras := []struct{ ask, want string }{
		{"", "2026-07-28"},
		{"2025-11-25", "2025-11-25"},
		{"2025-06-18", "2025-06-18"},
	}
	var baseline []byte
	for _, era := range eras {
		t.Run("era="+era.want, func(t *testing.T) {
			sess := startBridgeEra(t, eraConfig(t), era.ask)
			ir := sess.InitializeResult()
			if ir == nil || ir.ProtocolVersion != era.want {
				t.Fatalf("negotiated %+v, want %s", ir, era.want)
			}
			if !strings.Contains(ir.Instructions, "Era test agent") {
				t.Errorf("instructions not delivered: %q", ir.Instructions)
			}
			if ir.Capabilities == nil {
				t.Fatal("Capabilities is nil")
			}
			if ir.Capabilities.Logging != nil {
				t.Errorf("Logging capability advertised: %+v", ir.Capabilities.Logging)
			}
			if ir.Capabilities.Tools == nil {
				t.Error("Tools capability not advertised")
			}
			tl, err := sess.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(tl.Tools)
			if baseline == nil {
				baseline = got
			} else if string(got) != string(baseline) {
				t.Errorf("tools/list differs across eras:\n%s\nvs\n%s", got, baseline)
			}
			res, err := sess.CallTool(context.Background(), &gomcp.CallToolParams{Name: "alpha"})
			if err != nil || res.IsError {
				t.Fatalf("call alpha: err=%v res=%+v", err, res)
			}
		})
	}
}

// TestListsInvariantAcrossConnections is spec A4: two independent connections
// to the same binary must see byte-identical tools/list, prompts/list, and
// resources/list results, in sorted order. A memory-enabled config is used so
// resources/list is non-empty too (memory-context prompt, memory resources).
func TestListsInvariantAcrossConnections(t *testing.T) {
	listsConfig := func(t *testing.T) agentmcp.BridgeConfig {
		t.Helper()
		store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
		if err != nil {
			t.Fatalf("creating file store: %v", err)
		}
		mgr := memory.NewManager(store)
		if err := mgr.Set("k", "v"); err != nil {
			t.Fatalf("writing memory key: %v", err)
		}
		cfg := eraConfig(t)
		cfg.Memory = mgr
		return cfg
	}

	a := startBridgeEra(t, listsConfig(t), "")
	b := startBridgeEra(t, listsConfig(t), "")
	ctx := context.Background()

	la, err := a.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	lb, err := b.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ja, err := json.Marshal(la.Tools)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(lb.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) != string(jb) {
		t.Fatalf("tools/list varies per connection:\n%s\nvs\n%s", ja, jb)
	}
	for i := 1; i < len(la.Tools); i++ {
		if la.Tools[i-1].Name > la.Tools[i].Name {
			t.Fatalf("tools not sorted: %q before %q", la.Tools[i-1].Name, la.Tools[i].Name)
		}
	}

	pa, err := a.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pa.Prompts) == 0 {
		t.Fatal("prompts/list is empty; nothing to compare")
	}
	jpa, err := json.Marshal(pa.Prompts)
	if err != nil {
		t.Fatal(err)
	}
	jpb, err := json.Marshal(pb.Prompts)
	if err != nil {
		t.Fatal(err)
	}
	if string(jpa) != string(jpb) {
		t.Fatalf("prompts/list varies per connection:\n%s\nvs\n%s", jpa, jpb)
	}

	ra, err := a.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := b.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ra.Resources) == 0 {
		t.Fatal("resources/list is empty; nothing to compare")
	}
	jra, err := json.Marshal(ra.Resources)
	if err != nil {
		t.Fatal(err)
	}
	jrb, err := json.Marshal(rb.Resources)
	if err != nil {
		t.Fatal(err)
	}
	if string(jra) != string(jrb) {
		t.Fatalf("resources/list varies per connection:\n%s\nvs\n%s", jra, jrb)
	}
}

func TestInputValidationIsToolError(t *testing.T) {
	r := tools.NewRegistry()
	_ = r.Register(tools.BuiltinTool("needs_q", "needs q",
		map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}, "required": []string{"q"}},
		func(map[string]any) (string, error) { return "ok", nil }))
	cfg := eraConfig(t)
	cfg.Registry = r
	for _, era := range []string{"", "2025-11-25"} {
		sess := startBridgeEra(t, cfg, era)
		res, err := sess.CallTool(context.Background(), &gomcp.CallToolParams{Name: "needs_q", Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("era %q: protocol error %v, want isError result", era, err)
		}
		if !res.IsError {
			t.Fatalf("era %q: IsError=false for missing required arg", era)
		}
	}
}
