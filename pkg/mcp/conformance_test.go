package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
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

func TestToolsListInvariantAcrossConnections(t *testing.T) {
	a := startBridgeEra(t, eraConfig(t), "")
	b := startBridgeEra(t, eraConfig(t), "")
	la, _ := a.ListTools(context.Background(), nil)
	lb, _ := b.ListTools(context.Background(), nil)
	ja, _ := json.Marshal(la.Tools)
	jb, _ := json.Marshal(lb.Tools)
	if string(ja) != string(jb) {
		t.Fatalf("tools/list varies per connection")
	}
	for i := 1; i < len(la.Tools); i++ {
		if la.Tools[i-1].Name > la.Tools[i].Name {
			t.Fatalf("tools not sorted: %q before %q", la.Tools[i-1].Name, la.Tools[i].Name)
		}
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
