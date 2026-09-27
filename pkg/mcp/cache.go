package mcp

import (
	"context"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listTTLMs is the cache lifetime for list results. Tools, prompts, and
// resource listings are fixed for the lifetime of a binary, so any client
// may cache them for an hour.
const listTTLMs = 3_600_000

// setCacheable is the ServerOptions.SetCacheable hook. It runs with the
// server mutex held and must not call back into the server.
func setCacheable(_ context.Context, req gomcp.Request, c *gomcp.Cacheable) {
	switch req.(type) {
	case *gomcp.ListToolsRequest, *gomcp.ListPromptsRequest,
		*gomcp.ListResourcesRequest, *gomcp.ListResourceTemplatesRequest:
		c.TTLMs = listTTLMs
		c.CacheScope = "public"
	}
}
