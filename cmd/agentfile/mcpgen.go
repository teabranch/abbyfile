package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MCPServerEntry describes a single MCP server in the config.
type MCPServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// mergeMCPJSON reads an existing MCP config (if present), merges new entries,
// and writes it back. Preserves all existing keys and server entries that are
// not being overwritten. Creates parent directories as needed.
func mergeMCPJSON(path string, entries map[string]MCPServerEntry) error {
	// Use a generic map to preserve any unknown top-level or per-server fields.
	cfg := make(map[string]any)

	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	// Get or create the mcpServers map.
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = make(map[string]any)
	}

	// Merge new entries (overwrites existing entries with same key only).
	for k, v := range entries {
		servers[k] = v
	}
	cfg["mcpServers"] = servers

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Create parent directory if needed (for paths like ~/.claude/mcp.json).
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return os.WriteFile(path, append(out, '\n'), 0o644)
}
