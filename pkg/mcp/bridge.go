// Package mcp provides an MCP-over-stdio bridge for abbyfile binaries.
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
	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/prompt"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// BridgeConfig holds everything the MCP bridge needs to expose an agent.
type BridgeConfig struct {
	Name        string
	Version     string
	Description string
	Model       string // model hint/recommendation for the runtime
	Registry    *tools.Registry
	Executor    *tools.Executor
	Loader      *prompt.Loader
	Memory      *memory.Manager // nil if memory is disabled
	Logger      *slog.Logger    // nil disables logging

	// Deprecated: lazy tool loading was removed in v0.10.0. It listed tools
	// it never registered, and MCP 2026-07-28 requires a tools/list that
	// does not change per connection. The field is ignored; setting it
	// logs a warning. It will be removed in a future release.
	LazyToolLoading bool

	// EagerInstructions, when true, sends the full custom instructions in the
	// handshake (server/discover or initialize) and does not register
	// get_instructions. When false (the default), the handshake carries a
	// short stub and get_instructions serves the full text on demand.
	EagerInstructions bool
}

// metaMaxResultSizeChars is the Claude Code tool-definition _meta key that
// raises a tool's inline-result threshold (https://code.claude.com/docs/en/mcp.md).
const metaMaxResultSizeChars = "anthropic/maxResultSizeChars"

// Bridge translates an abbyfile tools.Registry into an MCP server.
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
	// Determine what to advertise as server instructions at handshake time.
	instructions := b.handshakeInstructions()

	server := gomcp.NewServer(&gomcp.Implementation{
		Name:    b.cfg.Name,
		Version: b.cfg.Version,
	}, &gomcp.ServerOptions{
		Instructions: instructions,
		SetCacheable: setCacheable,
		// Capabilities overrides go-sdk's default of advertising the
		// deprecated (SEP-2577) logging capability. An empty, non-nil
		// struct suppresses Logging while leaving Tools/Prompts/Resources
		// to be filled in by Server.capabilities() when those features are
		// actually registered (see server.go's capabilities()).
		Capabilities: &gomcp.ServerCapabilities{},
	})

	if b.cfg.LazyToolLoading {
		b.logger.Warn("BridgeConfig.LazyToolLoading is deprecated and ignored; all tools are registered")
	}

	// Register each abbyfile tool as an MCP tool.
	for _, def := range b.cfg.Registry.All() {
		b.addTool(server, def)
	}
	// A non-eager handshake carries only a stub pointing at get_instructions,
	// so the tool must exist. An eager handshake already carries the full
	// text, so the tool would only cost context.
	if !b.cfg.EagerInstructions {
		b.addGetInstructionsTool(server)
	}

	// Register memory resources if memory is enabled.
	if b.cfg.Memory != nil {
		b.addMemoryResources(server)
	}

	// Register prompt templates.
	b.addPrompts(server)

	b.logger.Info("starting MCP server", "name", b.cfg.Name, "version", b.cfg.Version, "tools", len(b.cfg.Registry.All()))
	return server.Run(ctx, transport)
}

// addTool registers a single abbyfile tool definition as an MCP tool.
func (b *Bridge) addTool(server *gomcp.Server, def *tools.Definition) {
	schema := inputSchemaToRaw(def.InputSchema)

	tool := &gomcp.Tool{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: schema,
	}

	// Pass through output schema if the tool defines one.
	if def.OutputSchema != nil {
		tool.OutputSchema = schemaToRaw(def.OutputSchema)
	}

	// Map abbyfile annotations to MCP tool annotations.
	if def.Annotations != nil {
		tool.Annotations = &gomcp.ToolAnnotations{
			ReadOnlyHint:    def.Annotations.ReadOnlyHint,
			DestructiveHint: def.Annotations.DestructiveHint,
			IdempotentHint:  def.Annotations.IdempotentHint,
			OpenWorldHint:   def.Annotations.OpenWorldHint,
			Title:           def.Annotations.Title,
		}
	}

	if chars, requested := b.cfg.Executor.ResultSizeHint(def.Name); chars > 0 {
		tool.Meta = gomcp.Meta{metaMaxResultSizeChars: chars}
	} else if requested {
		b.logger.Warn("inline_large ignored: tool's max_output_bytes is unlimited", "tool", def.Name)
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

		if d.OutputSchema != nil {
			raw, err := b.cfg.Executor.RunRaw(ctx, d, input)
			if err != nil {
				b.logger.Error("tool call failed", "tool", d.Name, "error", err)
				return errorResult(err.Error()), nil
			}
			return structuredResult(d.Name, raw, b.cfg.Executor.MaxOutputBytes(d.Name)), nil
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
// the agent's full instructions. Only registered when EagerInstructions is false.
func (b *Bridge) addGetInstructionsTool(server *gomcp.Server) {
	tool := &gomcp.Tool{
		Name:        "get_instructions",
		Description: "Load this agent's full instructions (system prompt). Call this before acting.",
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
		text = b.appendModelHint(text)
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
			Cacheable: gomcp.Cacheable{TTLMs: 0, CacheScope: "private"},
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
			Cacheable: gomcp.Cacheable{TTLMs: 0, CacheScope: "private"},
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
			// TODO: use mgr.FormatSummaryAsContext(4096) for richer context when available.
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

// instructionsStub is appended to the role line in a non-eager handshake.
// It must name only tools that are registered in that mode.
const instructionsStub = "Call the `get_instructions` tool to load your full instructions before acting."

// handshakeInstructions returns what the MCP handshake advertises. When
// EagerInstructions is false, it returns the role line plus instructionsStub;
// get_instructions serves the full prompt on demand.
func (b *Bridge) handshakeInstructions() string {
	full, _ := b.cfg.Loader.Load()
	full = b.appendModelHint(full)
	if b.cfg.EagerInstructions {
		return full
	}
	role := b.cfg.Description
	if role == "" {
		role = b.cfg.Name
	}
	return role + "\n\n" + instructionsStub
}

// appendModelHint appends a model preference section to instructions if a model is configured.
func (b *Bridge) appendModelHint(instructions string) string {
	if b.cfg.Model == "" {
		return instructions
	}
	return instructions + "\n\n## Model Preference\n\nThis agent was designed for model: " + b.cfg.Model
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

// inputSchemaToRaw is schemaToRaw plus a guarantee that the top-level schema
// declares "type":"object". It normalizes any non-"object" top-level type —
// missing, null, a different scalar (e.g. "string"), or a union like
// ["object","null"] — to "object". go-sdk v1.8.0's AddTool panics unless the
// decoded top-level "type" is exactly the string "object", and frontmatter
// authors routinely omit or mis-specify it.
func inputSchemaToRaw(schema any) json.RawMessage {
	raw := schemaToRaw(schema)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	if typ, ok := m["type"]; ok && typ == "object" {
		return raw
	}
	m["type"] = "object"
	out, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return out
}
