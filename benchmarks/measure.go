// Package benchmarks provides MCP token cost measurement and benchmarking for
// abbyfile agents. It measures tool schema overhead, system prompt size,
// and total context budget consumed by each agent.
package benchmarks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	agentmcp "github.com/teabranch/abbyfile/pkg/mcp"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// ContextWindow is the standard context window size used for budget calculations.
const ContextWindow = 128_000

// EstimateTokens returns a rough token count using the bytes/4 heuristic.
// This is not a real tokenizer but is consistent for relative comparisons.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// ToolMeasurement holds the serialized size of a single MCP tool.
type ToolMeasurement struct {
	Name         string `json:"name"`
	SchemaBytes  int    `json:"schema_bytes"`
	SchemaTokens int    `json:"schema_tokens"`
}

// RegistryMeasurement holds aggregate tool schema measurements.
type RegistryMeasurement struct {
	ToolCount    int               `json:"tool_count"`
	SchemaBytes  int               `json:"schema_bytes"`
	SchemaTokens int               `json:"schema_tokens"`
	PerTool      []ToolMeasurement `json:"per_tool"`
}

// mcpToolJSON mirrors the JSON structure of an MCP tool definition as sent
// over the wire during ListTools.
type mcpToolJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// MeasureRegistry serializes all tools as MCP-format JSON and counts bytes/tokens.
func MeasureRegistry(registry *tools.Registry) *RegistryMeasurement {
	defs := registry.All()
	m := &RegistryMeasurement{
		ToolCount: len(defs),
		PerTool:   make([]ToolMeasurement, 0, len(defs)),
	}

	for _, def := range defs {
		tool := mcpToolJSON{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		}
		data, _ := json.Marshal(tool)
		tm := ToolMeasurement{
			Name:         def.Name,
			SchemaBytes:  len(data),
			SchemaTokens: EstimateTokens(string(data)),
		}
		m.PerTool = append(m.PerTool, tm)
		m.SchemaBytes += len(data)
		m.SchemaTokens += tm.SchemaTokens
	}

	return m
}

// MeasureTools is a convenience function that measures a slice of tool definitions
// without requiring a Registry.
func MeasureTools(defs []*tools.Definition) *RegistryMeasurement {
	m := &RegistryMeasurement{
		ToolCount: len(defs),
		PerTool:   make([]ToolMeasurement, 0, len(defs)),
	}

	for _, def := range defs {
		tool := mcpToolJSON{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		}
		data, _ := json.Marshal(tool)
		tm := ToolMeasurement{
			Name:         def.Name,
			SchemaBytes:  len(data),
			SchemaTokens: EstimateTokens(string(data)),
		}
		m.PerTool = append(m.PerTool, tm)
		m.SchemaBytes += len(data)
		m.SchemaTokens += tm.SchemaTokens
	}

	return m
}

// HandshakePayload captures what the MCP handshake sends to the client.
type HandshakePayload struct {
	Tools             []ToolMeasurement `json:"tools"`
	TotalSchemaBytes  int               `json:"total_schema_bytes"`
	TotalSchemaTokens int               `json:"total_schema_tokens"`
	PromptBytes       int               `json:"prompt_bytes"`
	PromptTokens      int               `json:"prompt_tokens"`
	TotalBytes        int               `json:"total_bytes"`
	TotalTokens       int               `json:"total_tokens"`
}

// ContextBudgetPercent returns the percentage of the 128K context window consumed.
func (h *HandshakePayload) ContextBudgetPercent() float64 {
	return float64(h.TotalTokens) / float64(ContextWindow) * 100
}

// MeasureHandshake connects via in-memory transport, captures ListTools,
// and measures schema + prompt overhead.
func MeasureHandshake(cfg agentmcp.BridgeConfig) (*HandshakePayload, error) {
	bridge := agentmcp.NewBridge(cfg)

	serverTransport, clientTransport := gomcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- bridge.ServeTransport(ctx, serverTransport)
	}()

	client := gomcp.NewClient(&gomcp.Implementation{
		Name:    "bench-client",
		Version: "v0.1.0",
	}, nil)

	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to MCP bridge: %w", err)
	}
	defer session.Close()

	// List tools to capture what the client sees.
	listResult, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}

	payload := &HandshakePayload{}

	for _, tool := range listResult.Tools {
		data, _ := json.Marshal(tool)
		tm := ToolMeasurement{
			Name:         tool.Name,
			SchemaBytes:  len(data),
			SchemaTokens: EstimateTokens(string(data)),
		}
		payload.Tools = append(payload.Tools, tm)
		payload.TotalSchemaBytes += len(data)
		payload.TotalSchemaTokens += tm.SchemaTokens
	}

	// Measure system prompt.
	if cfg.Loader != nil {
		text, err := cfg.Loader.Load()
		if err == nil {
			payload.PromptBytes = len(text)
			payload.PromptTokens = EstimateTokens(text)
		}
	}

	payload.TotalBytes = payload.TotalSchemaBytes + payload.PromptBytes
	payload.TotalTokens = payload.TotalSchemaTokens + payload.PromptTokens

	return payload, nil
}
