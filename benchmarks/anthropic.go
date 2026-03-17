package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/teabranch/abbyfile/pkg/tools"
)

// AnthropicClient is a thin HTTP client for the Anthropic count_tokens API.
// It follows the same pattern as pkg/github/github.go — no new dependencies,
// just net/http and encoding/json.
type AnthropicClient struct {
	HTTPClient *http.Client
	BaseURL    string // defaults to "https://api.anthropic.com"
	APIKey     string
	Model      string // defaults to "claude-sonnet-4-6"
}

// AnthropicTool is the Anthropic API tool format.
// Note: Anthropic uses input_schema (snake_case), not inputSchema (camelCase like MCP).
type AnthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

// CountTokensRequest is the request body for POST /v1/messages/count_tokens.
type CountTokensRequest struct {
	Model    string          `json:"model"`
	Messages []AnthropicMsg  `json:"messages"`
	System   string          `json:"system,omitempty"`
	Tools    []AnthropicTool `json:"tools,omitempty"`
}

// AnthropicMsg is a message in the Anthropic API format.
type AnthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CountTokensResponse is the response from count_tokens.
type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// NewAnthropicClient reads ANTHROPIC_API_KEY from the environment and returns
// a client. The second return value is false if the key is not set.
func NewAnthropicClient() (*AnthropicClient, bool) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, false
	}
	return &AnthropicClient{
		HTTPClient: http.DefaultClient,
		BaseURL:    "https://api.anthropic.com",
		APIKey:     key,
		Model:      "claude-sonnet-4-6",
	}, true
}

// CountTokens calls the Anthropic count_tokens endpoint and returns the
// exact input token count. This is free — no inference cost.
func (c *AnthropicClient) CountTokens(ctx context.Context, req CountTokensRequest) (int, error) {
	if req.Model == "" {
		req.Model = c.Model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshaling count_tokens request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/messages/count_tokens", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("count_tokens request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("count_tokens API error (HTTP %d): %s", resp.StatusCode, respBody)
	}

	var result CountTokensResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("parsing count_tokens response: %w", err)
	}
	return result.InputTokens, nil
}

// ToolsToAnthropic converts MCP-format tool definitions to Anthropic API format.
// MCP uses "inputSchema" (camelCase); Anthropic uses "input_schema" (snake_case).
func ToolsToAnthropic(defs []*tools.Definition) []AnthropicTool {
	out := make([]AnthropicTool, len(defs))
	for i, def := range defs {
		out[i] = AnthropicTool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		}
	}
	return out
}

// BaselineToolsToAnthropic generates synthetic Anthropic tool definitions
// matching Claude Code's 23 built-in tools. Uses the tool names and estimated
// token counts from EstimateClaudeCodeBaseline() to create representative schemas.
func BaselineToolsToAnthropic() []AnthropicTool {
	baseline := EstimateClaudeCodeBaseline()
	out := make([]AnthropicTool, 0, len(baseline.BuiltinTools))
	for name, tokens := range baseline.BuiltinTools {
		// Generate a description sized to approximate the token count.
		// Each tool's description in Claude Code is substantial (usage rules, examples).
		descLen := tokens * 4 // ~4 bytes per token
		desc := generateDescription(name, descLen)
		out = append(out, AnthropicTool{
			Name:        name,
			Description: desc,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		})
	}
	return out
}

// generateDescription creates a representative description of approximately
// the given byte length for a tool with the given name.
func generateDescription(name string, targetLen int) string {
	base := fmt.Sprintf("The %s tool provides functionality for Claude Code operations. ", name)
	for len(base) < targetLen {
		base += fmt.Sprintf("This tool handles %s-related tasks with appropriate safety checks and validation. ", name)
	}
	if len(base) > targetLen {
		base = base[:targetLen]
	}
	return base
}
