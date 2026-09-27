package mcp_test

import (
	"context"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func TestListResultsAreCacheable(t *testing.T) {
	session, _ := startBridge(t, tools.NewRegistry())
	ctx := context.Background()

	tl, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tl.TTLMs != 3_600_000 || tl.CacheScope != "public" {
		t.Errorf("tools/list cache = %d/%q, want 3600000/public", tl.TTLMs, tl.CacheScope)
	}
	pl, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pl.TTLMs != 3_600_000 || pl.CacheScope != "public" {
		t.Errorf("prompts/list cache = %d/%q", pl.TTLMs, pl.CacheScope)
	}
}

func TestMemoryReadIsPrivateAndUncached(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	mgr := memory.NewManager(store)
	if err := mgr.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0",
		Registry: tools.NewRegistry(), Executor: tools.NewExecutor(30*time.Second, nil),
		Loader: newTestLoader(t), Memory: mgr,
	})
	res, err := session.ReadResource(context.Background(), &gomcp.ReadResourceParams{URI: "memory://test-agent/k"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TTLMs != 0 || res.CacheScope != "private" {
		t.Errorf("memory read cache = %d/%q, want 0/private", res.TTLMs, res.CacheScope)
	}
}
