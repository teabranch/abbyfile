package mcp_test

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/agentfile/pkg/mcp"
	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
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

func TestBridgeLazyToolLoadingSearchTools(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.BuiltinTool(
		"echo",
		"Echo back the input message",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
		},
		func(input map[string]any) (string, error) {
			msg, _ := input["message"].(string)
			return "echo: " + msg, nil
		},
	))
	_ = registry.Register(tools.BuiltinTool(
		"file_read",
		"Read a file from disk",
		map[string]any{"type": "object", "properties": map[string]any{}},
		func(input map[string]any) (string, error) { return "content", nil },
	))

	session, _ := startBridgeWithConfig(t, agentmcp.BridgeConfig{
		Name:            "test-agent",
		Version:         "v0.1.0",
		Registry:        registry,
		Executor:        tools.NewExecutor(30*time.Second, nil),
		Loader:          newTestLoader(t),
		LazyToolLoading: true,
	})
	ctx := context.Background()

	// List tools — in lazy mode should only see search_tools + get_instructions = 2.
	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listResult.Tools) != 2 {
		names := make([]string, len(listResult.Tools))
		for i, tool := range listResult.Tools {
			names[i] = tool.Name
		}
		t.Fatalf("expected 2 tools in lazy mode, got %d: %v", len(listResult.Tools), names)
	}

	var foundSearch, foundInstructions bool
	for _, tool := range listResult.Tools {
		switch tool.Name {
		case "search_tools":
			foundSearch = true
		case "get_instructions":
			foundInstructions = true
		}
	}
	if !foundSearch {
		t.Error("search_tools not found in lazy mode")
	}
	if !foundInstructions {
		t.Error("get_instructions not found in lazy mode")
	}

	// Call search_tools with a query matching "echo".
	result, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "search_tools",
		Arguments: map[string]any{"query": "echo"},
	})
	if err != nil {
		t.Fatalf("call search_tools: %v", err)
	}
	if result.IsError {
		t.Fatalf("search_tools returned error: %s", extractText(result))
	}
	text := extractText(result)
	if !strings.Contains(text, "echo") {
		t.Errorf("search result should contain 'echo', got %q", text)
	}
	if strings.Contains(text, "file_read") {
		t.Errorf("search result should not contain 'file_read' for query 'echo', got %q", text)
	}

	// Call search_tools with a query matching by description.
	result2, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "search_tools",
		Arguments: map[string]any{"query": "file"},
	})
	if err != nil {
		t.Fatalf("call search_tools (file): %v", err)
	}
	text2 := extractText(result2)
	if !strings.Contains(text2, "file_read") {
		t.Errorf("search result should contain 'file_read', got %q", text2)
	}

	// Call search_tools with no matches.
	result3, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "search_tools",
		Arguments: map[string]any{"query": "nonexistent_xyz"},
	})
	if err != nil {
		t.Fatalf("call search_tools (no match): %v", err)
	}
	text3 := extractText(result3)
	if !strings.Contains(text3, "No tools matched") {
		t.Errorf("expected 'No tools matched' message, got %q", text3)
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
