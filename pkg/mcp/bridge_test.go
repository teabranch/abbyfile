package mcp_test

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/prompt"
	"github.com/teabranch/abbyfile/pkg/tools"
)

//go:embed testdata/system.md
var testPromptFS embed.FS

func newTestLoader(t *testing.T) *prompt.Loader {
	t.Helper()
	return prompt.NewLoader("test-agent", testPromptFS, "testdata/system.md")
}

// startBridgeWithConfig creates and starts a bridge with the given config, returning
// a connected client session.
func startBridgeWithConfig(t *testing.T, cfg agentmcp.BridgeConfig) (session *gomcp.ClientSession, cancel context.CancelFunc) {
	t.Helper()

	bridge := agentmcp.NewBridge(cfg)

	serverTransport, clientTransport := gomcp.NewInMemoryTransports()

	ctx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)

	go func() {
		_ = bridge.ServeTransport(ctx, serverTransport)
	}()

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "test-client",
		Version: "v0.1.0",
	}, nil)

	sess, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancelFn()
		t.Fatalf("client connect: %v", err)
	}

	t.Cleanup(func() {
		sess.Close()
		cancelFn()
	})

	return sess, cancelFn
}

// startBridgeEra is startBridgeWithConfig with an explicit client protocol
// version ("" = SDK latest, i.e. 2026-07-28 via server/discover).
func startBridgeEra(t *testing.T, cfg agentmcp.BridgeConfig, protocolVersion string) *gomcp.ClientSession {
	t.Helper()
	bridge := agentmcp.NewBridge(cfg)
	serverTransport, clientTransport := gomcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() { _ = bridge.ServeTransport(ctx, serverTransport) }()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "era-client", Version: "v0.1.0"}, nil)
	sess, err := client.Connect(ctx, clientTransport, &gomcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		cancel()
		t.Fatalf("connect (%q): %v", protocolVersion, err)
	}
	t.Cleanup(func() { sess.Close(); cancel() })
	return sess
}

// startBridge creates and starts a bridge with the given registry, returning
// a connected client session. Delegates to startBridgeWithConfig.
func startBridge(t *testing.T, registry *tools.Registry) (session *gomcp.ClientSession, cancel context.CancelFunc) {
	t.Helper()
	return startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:     "test-agent",
		Version:  "v0.1.0",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   newTestLoader(t),
	})
}

func TestBridgeServesTools(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool(
		"echo",
		"Echo back the input message",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "The message to echo",
				},
			},
			"required": []string{"message"},
		},
		func(input map[string]any) (string, error) {
			msg, _ := input["message"].(string)
			return "echo: " + msg, nil
		},
	))

	session, _ := startBridge(t, registry)
	ctx := context.Background()

	// List tools — should see echo + get_instructions = 2.
	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listResult.Tools) != 2 {
		names := make([]string, len(listResult.Tools))
		for i, t := range listResult.Tools {
			names[i] = t.Name
		}
		t.Fatalf("expected 2 tools, got %d: %v", len(listResult.Tools), names)
	}

	// Verify tool presence.
	var foundEcho, foundInstructions bool
	for _, tool := range listResult.Tools {
		switch tool.Name {
		case "echo":
			foundEcho = true
			if tool.Description != "Echo back the input message" {
				t.Errorf("echo description = %q", tool.Description)
			}
		case "get_instructions":
			foundInstructions = true
		}
	}
	if !foundEcho {
		t.Error("echo tool not found")
	}
	if !foundInstructions {
		t.Error("get_instructions tool not found")
	}
}

func TestBridgeCallTool(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool(
		"echo",
		"Echo back the input message",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		func(input map[string]any) (string, error) {
			msg, _ := input["message"].(string)
			return "echo: " + msg, nil
		},
	))

	session, _ := startBridge(t, registry)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}
	if result.IsError {
		t.Fatalf("echo returned error: %v", result.Content)
	}
	text := extractText(result)
	if text != "echo: hello" {
		t.Errorf("echo result = %q, want %q", text, "echo: hello")
	}
}

func TestBridgeGetInstructions(t *testing.T) {
	registry := tools.NewRegistry()
	session, _ := startBridge(t, registry)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "get_instructions",
	})
	if err != nil {
		t.Fatalf("call get_instructions: %v", err)
	}
	text := extractText(result)
	if text != "You are a test agent for MCP bridge testing." {
		t.Errorf("instructions = %q", text)
	}
}

func TestBridgeHandlesToolError(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool(
		"fail",
		"Always fails",
		map[string]any{"type": "object", "properties": map[string]any{}},
		func(input map[string]any) (string, error) {
			return "", fmt.Errorf("intentional failure")
		},
	))

	session, _ := startBridge(t, registry)
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "fail",
	})
	if err != nil {
		t.Fatalf("call fail: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for failing tool")
	}
	text := extractText(result)
	if text == "" {
		t.Error("expected error message in result")
	}
}

func TestBridgeToolAnnotations(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool(
		"safe_read",
		"A read-only tool",
		map[string]any{"type": "object", "properties": map[string]any{}},
		func(input map[string]any) (string, error) { return "ok", nil },
	).WithAnnotations(&tools.Annotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  tools.BoolPtr(false),
		Title:          "Safe Read",
	}))

	session, _ := startBridge(t, registry)
	ctx := context.Background()

	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var found bool
	for _, tool := range listResult.Tools {
		if tool.Name == "safe_read" {
			found = true
			if tool.Annotations == nil {
				t.Fatal("expected annotations on safe_read tool")
			}
			if !tool.Annotations.ReadOnlyHint {
				t.Error("expected ReadOnlyHint=true")
			}
			if !tool.Annotations.IdempotentHint {
				t.Error("expected IdempotentHint=true")
			}
			if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
				t.Error("expected OpenWorldHint=false")
			}
			if tool.Annotations.Title != "Safe Read" {
				t.Errorf("title = %q, want %q", tool.Annotations.Title, "Safe Read")
			}
		}
	}
	if !found {
		t.Error("safe_read tool not found")
	}
}

func TestBridgeMemoryResources(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatalf("creating file store: %v", err)
	}
	mgr := memory.NewManager(store)

	// Write a test key.
	if err := mgr.Set("test-key", "test-value"); err != nil {
		t.Fatalf("writing memory key: %v", err)
	}

	registry := tools.NewRegistry()
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:     "test-agent",
		Version:  "v0.1.0",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   newTestLoader(t),
		Memory:   mgr,
	})
	ctx := context.Background()

	// List resources — should include the memory index.
	listResult, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	var foundIndex bool
	for _, r := range listResult.Resources {
		if r.URI == "memory://test-agent/" {
			foundIndex = true
		}
	}
	if !foundIndex {
		t.Error("memory index resource not found")
	}

	// Read index resource.
	indexResult, err := session.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "memory://test-agent/",
	})
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if len(indexResult.Contents) == 0 {
		t.Fatal("empty index contents")
	}
	indexText := indexResult.Contents[0].Text
	if !strings.Contains(indexText, "test-key") {
		t.Errorf("index does not contain test-key: %s", indexText)
	}

	// Read individual key via resource template.
	keyResult, err := session.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "memory://test-agent/test-key",
	})
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if len(keyResult.Contents) == 0 {
		t.Fatal("empty key contents")
	}
	if keyResult.Contents[0].Text != "test-value" {
		t.Errorf("key value = %q, want %q", keyResult.Contents[0].Text, "test-value")
	}
}

func TestBridgePrompts(t *testing.T) {
	registry := tools.NewRegistry()
	session, _ := startBridge(t, registry)
	ctx := context.Background()

	// List prompts — should include "system" (no memory, so no memory-context).
	listResult, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}

	var foundSystem bool
	for _, p := range listResult.Prompts {
		if p.Name == "system" {
			foundSystem = true
		}
		if p.Name == "memory-context" {
			t.Error("memory-context prompt should not be present without memory")
		}
	}
	if !foundSystem {
		t.Error("system prompt not found")
	}

	// Get system prompt.
	result, err := session.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "system",
	})
	if err != nil {
		t.Fatalf("get system prompt: %v", err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("system prompt has no messages")
	}
	tc, ok := result.Messages[0].Content.(*gomcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in system prompt message")
	}
	if tc.Text != "You are a test agent for MCP bridge testing." {
		t.Errorf("system prompt text = %q", tc.Text)
	}
}

func TestBridgeMemoryContextPrompt(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatalf("creating file store: %v", err)
	}
	mgr := memory.NewManager(store)

	// Write a test key.
	if err := mgr.Set("notes", "important stuff"); err != nil {
		t.Fatalf("writing memory key: %v", err)
	}

	registry := tools.NewRegistry()
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:     "test-agent",
		Version:  "v0.1.0",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   newTestLoader(t),
		Memory:   mgr,
	})
	ctx := context.Background()

	// List prompts — should include memory-context.
	listResult, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	var foundMemCtx bool
	for _, p := range listResult.Prompts {
		if p.Name == "memory-context" {
			foundMemCtx = true
		}
	}
	if !foundMemCtx {
		t.Error("memory-context prompt not found")
	}

	// Get memory-context prompt with specific key.
	result, err := session.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name:      "memory-context",
		Arguments: map[string]string{"key": "notes"},
	})
	if err != nil {
		t.Fatalf("get memory-context prompt: %v", err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("memory-context prompt has no messages")
	}
	tc, ok := result.Messages[0].Content.(*gomcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in memory-context prompt")
	}
	if !strings.Contains(tc.Text, "important stuff") {
		t.Errorf("memory-context text = %q, want to contain 'important stuff'", tc.Text)
	}

	// Get memory-context prompt without key (summary).
	summaryResult, err := session.GetPrompt(ctx, &gomcp.GetPromptParams{
		Name: "memory-context",
	})
	if err != nil {
		t.Fatalf("get memory-context summary: %v", err)
	}
	if len(summaryResult.Messages) == 0 {
		t.Fatal("memory-context summary has no messages")
	}
	stc, ok := summaryResult.Messages[0].Content.(*gomcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in summary")
	}
	if !strings.Contains(stc.Text, "notes") {
		t.Errorf("summary text = %q, want to contain 'notes'", stc.Text)
	}
}

func TestBridgeLazyToolLoadingIgnored(t *testing.T) { // Review Focus #3
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool("echo", "Echo back the input message",
		map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
		func(input map[string]any) (string, error) {
			msg, _ := input["message"].(string)
			return "echo: " + msg, nil
		}))

	var logBuf bytes.Buffer
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:            "test-agent",
		Version:         "v0.1.0",
		Registry:        registry,
		Executor:        tools.NewExecutor(30*time.Second, nil),
		Loader:          newTestLoader(t),
		Logger:          slog.New(slog.NewTextHandler(&logBuf, nil)),
		LazyToolLoading: true,
	})
	ctx := context.Background()

	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	if names["search_tools"] {
		t.Error("search_tools must not be registered")
	}
	if !names["echo"] {
		t.Errorf("echo must be listed even with LazyToolLoading set; got %v", names)
	}

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "echo", Arguments: map[string]any{"message": "hi"}})
	if err != nil || res.IsError {
		t.Fatalf("echo call: err=%v isError=%v", err, res != nil && res.IsError)
	}
	if got := extractText(res); got != "echo: hi" {
		t.Errorf("echo = %q", got)
	}
	if !strings.Contains(logBuf.String(), "LazyToolLoading is deprecated") {
		t.Errorf("expected deprecation warning, log was: %q", logBuf.String())
	}
}

func TestBridgeModelHintInInstructions(t *testing.T) {
	registry := tools.NewRegistry()
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:     "test-agent",
		Version:  "v0.1.0",
		Model:    "claude-opus-4-6",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   newTestLoader(t),
	})
	ctx := context.Background()

	// get_instructions should include model hint.
	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "get_instructions",
	})
	if err != nil {
		t.Fatalf("call get_instructions: %v", err)
	}
	text := extractText(result)
	if !strings.Contains(text, "claude-opus-4-6") {
		t.Errorf("instructions should contain model hint, got: %s", text)
	}
	if !strings.Contains(text, "Model Preference") {
		t.Errorf("instructions should contain 'Model Preference' header, got: %s", text)
	}
}

func TestBridgeNoModelHintWhenEmpty(t *testing.T) {
	registry := tools.NewRegistry()
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:     "test-agent",
		Version:  "v0.1.0",
		Registry: registry,
		Executor: tools.NewExecutor(30*time.Second, nil),
		Loader:   newTestLoader(t),
	})
	ctx := context.Background()

	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "get_instructions",
	})
	if err != nil {
		t.Fatalf("call get_instructions: %v", err)
	}
	text := extractText(result)
	if strings.Contains(text, "Model Preference") {
		t.Errorf("instructions should NOT contain model hint when model is empty, got: %s", text)
	}
}

func TestStructuredOutputEndToEnd(t *testing.T) {
	r := tools.NewRegistry()
	def := tools.BuiltinTool("stats", "stats", map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return "{\"count\":3}\n", nil })
	def.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"count": map[string]any{"type": "integer"}}}
	_ = r.Register(def)

	session, _ := startBridge(t, r)
	res, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "stats"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("isError: %+v", res.Content)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok || m["count"] != float64(3) {
		t.Fatalf("structuredContent = %#v", res.StructuredContent)
	}
}

func TestStructuredOutputToolFailure(t *testing.T) { // Review Focus #4
	r := tools.NewRegistry()
	def := tools.BuiltinTool("broken", "broken", map[string]any{"type": "object"},
		func(map[string]any) (string, error) { return "", fmt.Errorf("boom") })
	def.OutputSchema = map[string]any{"type": "object"}
	_ = r.Register(def)

	session, _ := startBridge(t, r)
	res, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "broken"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || res.StructuredContent != nil {
		t.Fatalf("want plain isError, got isError=%v structured=%#v", res.IsError, res.StructuredContent)
	}
	if tc, _ := res.Content[0].(*gomcp.TextContent); tc == nil || !strings.Contains(tc.Text, "boom") {
		t.Fatalf("error text = %+v", res.Content)
	}
}

func extractText(result *gomcp.CallToolResult) string {
	for _, c := range result.Content {
		if tc, ok := c.(*gomcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestBridgeResultSizeHintMeta(t *testing.T) {
	r := tools.NewRegistry()
	noop := func(map[string]any) (string, error) { return "", nil }
	for _, name := range []string{"big", "plain"} {
		_ = r.Register(tools.BuiltinTool(name, name, map[string]any{"type": "object"}, noop))
	}
	b := tools.DefaultContextBudget()
	b.PerTool = map[string]tools.ContextBudget{"big": {InlineLarge: true, MaxOutputBytes: 300000}}

	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0", Registry: r,
		Executor: tools.NewExecutor(30*time.Second, nil, tools.WithContextBudget(b, nil)),
		Loader:   newTestLoader(t),
	})
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		got, has := tool.Meta["anthropic/maxResultSizeChars"]
		switch tool.Name {
		case "big":
			if n, ok := got.(float64); !ok || n != 300000 {
				t.Errorf("big _meta = %#v, want anthropic/maxResultSizeChars=300000", tool.Meta)
			}
		default:
			if has {
				t.Errorf("%s must not carry maxResultSizeChars: %#v", tool.Name, tool.Meta)
			}
		}
	}
}

// TestBridgeInlineLargeIgnoredWarnsWhenUnlimited covers M5: a tool that opts
// into inline_large under a base budget with an unlimited (0) MaxOutputBytes
// must not advertise anthropic/maxResultSizeChars (there's no cap to hint),
// and addTool must log a warning explaining why the opt-in was ignored.
func TestBridgeInlineLargeIgnoredWarnsWhenUnlimited(t *testing.T) {
	r := tools.NewRegistry()
	noop := func(map[string]any) (string, error) { return "", nil }
	_ = r.Register(tools.BuiltinTool("t", "t", map[string]any{"type": "object"}, noop))

	b := tools.ContextBudget{
		MaxOutputBytes: 0, // unlimited at the base level.
		OnOverflow:     tools.OverflowHeadTail,
		PerTool:        map[string]tools.ContextBudget{"t": {InlineLarge: true}},
	}

	var logBuf bytes.Buffer
	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name: "test-agent", Version: "v0.1.0", Registry: r,
		Executor: tools.NewExecutor(30*time.Second, nil, tools.WithContextBudget(b, nil)),
		Loader:   newTestLoader(t),
		Logger:   slog.New(slog.NewTextHandler(&logBuf, nil)),
	})

	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		if tool.Name != "t" {
			continue
		}
		if _, has := tool.Meta["anthropic/maxResultSizeChars"]; has {
			t.Errorf("t must not carry maxResultSizeChars when unlimited: %#v", tool.Meta)
		}
	}
	if !strings.Contains(logBuf.String(), "inline_large ignored") {
		t.Errorf("expected an 'inline_large ignored' warning in the log, got: %q", logBuf.String())
	}
}
