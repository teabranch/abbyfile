package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// structuredResult builds a CallToolResult for a tool that declares an
// outputSchema. The raw output must be a single JSON value (any type, per
// SEP-2106). Structured content is never truncated: output over maxBytes
// (when > 0) is an error rather than a corrupted JSON value. A text copy is
// included for clients that ignore structuredContent.
func structuredResult(toolName, raw string, maxBytes int64) *gomcp.CallToolResult {
	trimmed := strings.TrimSpace(raw)
	if maxBytes > 0 && int64(len(trimmed)) > maxBytes {
		return errorResult(fmt.Sprintf(
			"tool %q structured output is %d bytes, exceeding the %d-byte cap; structured output cannot be truncated (raise context_budget.per_tool.%s.max_output_bytes)",
			toolName, len(trimmed), maxBytes, toolName))
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return errorResult(fmt.Sprintf("tool %q declared outputSchema but produced non-JSON output: %v", toolName, err))
	}
	return &gomcp.CallToolResult{
		Content:           []gomcp.Content{&gomcp.TextContent{Text: trimmed}},
		StructuredContent: value,
	}
}
