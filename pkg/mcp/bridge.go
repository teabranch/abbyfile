// Package mcp provides an MCP-over-stdio bridge for agentfile binaries.
// It translates a tools.Registry into MCP tools so Claude Code can discover
// and invoke them via the Model Context Protocol. It also exposes server
// instructions, tool annotations, memory resources, and prompt templates.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teabranch/agentfile/pkg/memory"
	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

// BridgeConfig holds everything the MCP bridge needs to expose an agent.
type BridgeConfig struct {
	Name        string
	Version     string
	Description string
	Registry    *tools.Registry
	Executor    *tools.Executor
	Loader      *prompt.Loader
	Memory      *memory.Manager // nil if memory is disabled
	Logger      *slog.Logger    // nil disables logging
}

// Bridge translates an agentfile tools.Registry into an MCP server.
type Bridge struct {
	cfg    BridgeConfig
	logger *slog.Logger
}

// NewBridge creates a new MCP bridge from a BridgeConfig.
func NewBridge(cfg BridgeConfig) *Bridge {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Bridge{cfg: cfg, logger: logger}
}

// Serve starts the MCP server over stdio. It blocks until the context is
// cancelled or the transport closes.
func (b *Bridge) Serve(ctx context.Context) error {
	return b.ServeTransport(ctx, &gomcp.StdioTransport{})
}

// ServeTransport starts the MCP server on the given transport. This is useful
// for testing with in-memory transports.
func (b *Bridge) ServeTransport(ctx context.Context, transport gomcp.Transport) error {
	// Load the system prompt for server instructions.
	instructions, _ := b.cfg.Loader.Load()

	server := gomcp.NewServer(&gomcp.Implementation{
		Name:    b.cfg.Name,
		Version: b.cfg.Version,
	}, &gomcp.ServerOptions{
		Instructions: instructions,
	})

	// Register each agentfile tool as an MCP tool.
	for _, def := range b.cfg.Registry.All() {
		b.addTool(server, def)
	}

	// Register the special get_instructions tool (backward compatibility).
	b.addGetInstructionsTool(server)

	// Register memory resources if memory is enabled.
	if b.cfg.Memory != nil {
		b.addMemoryResources(server)
	}

	// Register prompt templates.
	b.addPrompts(server)

	b.logger.Info("starting MCP server", "name", b.cfg.Name, "version", b.cfg.Version, "tools", len(b.cfg.Registry.All()))
	return server.Run(ctx, transport)
}

// addTool registers a single agentfile tool definition as an MCP tool.
func (b *Bridge) addTool(server *gomcp.Server, def *tools.Definition) {
	schema := schemaToRaw(def.InputSchema)

	tool := &gomcp.Tool{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: schema,
	}

	// Map agentfile annotations to MCP tool annotations.
	if def.Annotations != nil {
		tool.Annotations = &gomcp.ToolAnnotations{
			ReadOnlyHint:    def.Annotations.ReadOnlyHint,
			DestructiveHint: def.Annotations.DestructiveHint,
			IdempotentHint:  def.Annotations.IdempotentHint,
			OpenWorldHint:   def.Annotations.OpenWorldHint,
			Title:           def.Annotations.Title,
		}
	}

	// Capture def for the closure.
	d := def
	server.AddTool(tool, func(ctx context.Context, req *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		b.logger.Debug("tool call received", "tool", d.Name)

		var input map[string]any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
				return errorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
			}
		}

		if err := d.ValidateInput(input); err != nil {
			return errorResult(fmt.Sprintf("invalid input: %v", err)), nil
		}

		result, err := b.cfg.Executor.Run(ctx, d, input)
		if err != nil {
			b.logger.Error("tool call failed", "tool", d.Name, "error", err)
			return errorResult(err.Error()), nil
		}

		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: result}},
		}, nil
	})
}

// addGetInstructionsTool registers the get_instructions tool that returns
// the agent's system prompt. Kept for backward compatibility.
func (b *Bridge) addGetInstructionsTool(server *gomcp.Server) {
	tool := &gomcp.Tool{
		Name:        "get_instructions",
		Description: "Get the agent's system prompt / custom instructions. Deprecated: use server instructions (handshake) or the 'system' prompt instead.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Annotations: &gomcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			Title:          "Get Instructions",
		},
	}

	server.AddTool(tool, func(ctx context.Context, req *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		text, err := b.cfg.Loader.Load()
		if err != nil {
			return errorResult(fmt.Sprintf("failed to load instructions: %v", err)), nil
		}
		return &gomcp.CallToolResult{
			Content: []gomcp.Content{&gomcp.TextContent{Text: text}},
		}, nil
	})
}

// addMemoryResources registers memory keys as MCP resources.
// - Static resource: memory://<name>/ — JSON index of all current keys
// - Resource template: memory://<name>/{key} — reads individual keys
func (b *Bridge) addMemoryResources(server *gomcp.Server) {
	name := b.cfg.Name
	mgr := b.cfg.Memory

	// Static index resource.
	indexURI := fmt.Sprintf("memory://%s/", name)
	server.AddResource(&gomcp.Resource{
		URI:         indexURI,
		Name:        "memory-index",
		Description: "JSON index of all memory keys for " + name,
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		keys, err := mgr.Keys()
		if err != nil {
			return nil, fmt.Errorf("listing memory keys: %w", err)
		}
		data, _ := json.Marshal(keys)
		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{{
				URI:      indexURI,
				MIMEType: "application/json",
				Text:     string(data),
			}},
		}, nil
	})

	// Resource template for individual keys.
	templateURI := fmt.Sprintf("memory://%s/{key}", name)
	server.AddResourceTemplate(&gomcp.ResourceTemplate{
		URITemplate: templateURI,
		Name:        "memory-key",
		Description: "Read a specific memory key for " + name,
		MIMEType:    "text/plain",
	}, func(ctx context.Context, req *gomcp.ReadResourceRequest) (*gomcp.ReadResourceResult, error) {
		// Extract key from URI: memory://<name>/<key>
		prefix := fmt.Sprintf("memory://%s/", name)
		key := strings.TrimPrefix(req.Params.URI, prefix)
		if key == "" {
			return nil, fmt.Errorf("missing key in URI %q", req.Params.URI)
		}

		value, err := mgr.Get(key)
		if err != nil {
			return nil, fmt.Errorf("reading memory key %q: %w", key, err)
		}

		return &gomcp.ReadResourceResult{
			Contents: []*gomcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "text/plain",
				Text:     value,
			}},
		}, nil
	})
}

// addPrompts registers MCP prompt templates.
// - "system" — returns the agent's system prompt
// - "memory-context" (only if memory enabled) — returns memory state
func (b *Bridge) addPrompts(server *gomcp.Server) {
	// System prompt.
	server.AddPrompt(&gomcp.Prompt{
		Name:        "system",
		Description: "The agent's system prompt / custom instructions",
	}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
		text, err := b.cfg.Loader.Load()
		if err != nil {
			return nil, fmt.Errorf("loading system prompt: %w", err)
		}
		return &gomcp.GetPromptResult{
			Description: "System prompt for " + b.cfg.Name,
			Messages: []*gomcp.PromptMessage{{
				Role:    "user",
				Content: &gomcp.TextContent{Text: text},
			}},
		}, nil
	})

	// Memory-context prompt (only when memory is enabled).
	if b.cfg.Memory != nil {
		mgr := b.cfg.Memory
		server.AddPrompt(&gomcp.Prompt{
			Name:        "memory-context",
			Description: "Current memory state for context injection",
			Arguments: []*gomcp.PromptArgument{{
				Name:        "key",
				Description: "Specific memory key to include (omit for all keys summary)",
			}},
		}, func(ctx context.Context, req *gomcp.GetPromptRequest) (*gomcp.GetPromptResult, error) {
			key := req.Params.Arguments["key"]

			if key != "" {
				// Return a specific key's content.
				value, err := mgr.Get(key)
				if err != nil {
					return nil, fmt.Errorf("reading memory key %q: %w", key, err)
				}
				return &gomcp.GetPromptResult{
					Description: fmt.Sprintf("Memory key %q", key),
					Messages: []*gomcp.PromptMessage{{
						Role:    "user",
						Content: &gomcp.TextContent{Text: fmt.Sprintf("Memory [%s]:\n%s", key, value)},
					}},
				}, nil
			}

			// Return summary of all keys.
			summary := mgr.FormatKeysAsContext()
			if summary == "" {
				summary = "No keys in memory."
			}
			return &gomcp.GetPromptResult{
				Description: "Memory context for " + b.cfg.Name,
				Messages: []*gomcp.PromptMessage{{
					Role:    "user",
					Content: &gomcp.TextContent{Text: summary},
				}},
			}, nil
		})
	}
}

// errorResult creates a CallToolResult with IsError set.
func errorResult(msg string) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{&gomcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// schemaToRaw converts a tools.Definition InputSchema (typically map[string]any)
// to json.RawMessage for the MCP SDK.
func schemaToRaw(schema any) json.RawMessage {
	if schema == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	if raw, ok := schema.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return data
}
