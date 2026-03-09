package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/teabranch/agentfile/pkg/tools"
)

// Manager provides concurrency-safe access to a FileStore and exposes
// memory operations as built-in tools for the agentic loop.
type Manager struct {
	store *FileStore
	mu    sync.RWMutex
}

// NewManager creates a Manager backed by the given FileStore.
func NewManager(store *FileStore) *Manager {
	return &Manager{store: store}
}

// Get reads a value by key.
func (m *Manager) Get(key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.Read(key)
}

// Set writes a value by key.
func (m *Manager) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Write(key, value)
}

// Append appends content to a key.
func (m *Manager) Append(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Append(key, value)
}

// Delete removes a key.
func (m *Manager) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Delete(key)
}

// Keys lists all stored keys.
func (m *Manager) Keys() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.Keys()
}

// Search performs substring matching across all values.
func (m *Manager) Search(pattern string) ([]SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.Search(pattern)
}

// GC deletes expired keys (based on TTL). Returns count of deleted keys.
func (m *Manager) GC() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys, err := m.store.Keys()
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, key := range keys {
		if err := m.store.checkExpired(key); err != nil && errors.Is(err, ErrExpired) {
			if delErr := m.store.Delete(key); delErr == nil {
				deleted++
			}
		}
	}
	return deleted, nil
}

// FormatSummaryAsContext returns key names + first 200 bytes of each value
// (truncated with "[truncated]"), stopping when total bytes would exceed maxTotalBytes.
func (m *Manager) FormatSummaryAsContext(maxTotalBytes int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys, err := m.store.Keys()
	if err != nil || len(keys) == 0 {
		return ""
	}

	var buf strings.Builder
	total := 0

	for _, key := range keys {
		val, err := m.store.Read(key)
		if err != nil {
			continue
		}

		preview := val
		truncated := false
		if len(preview) > 200 {
			preview = preview[:200]
			truncated = true
		}

		line := key + ": " + preview
		if truncated {
			line += "[truncated]"
		}
		line += "\n"

		if total+len(line) > maxTotalBytes {
			break
		}
		buf.WriteString(line)
		total += len(line)
	}
	return buf.String()
}

// Tools returns the built-in tool definitions for memory operations.
func (m *Manager) Tools() []*tools.Definition {
	closedWorld := tools.BoolPtr(false)
	nonDestructive := tools.BoolPtr(false)

	return []*tools.Definition{
		tools.BuiltinTool(
			"memory_read",
			"Read a value from the agent's persistent memory by key",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{
						"type":        "string",
						"description": "The memory key to read",
					},
				},
				"required": []string{"key"},
			},
			m.handleRead,
		).WithAnnotations(&tools.Annotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  closedWorld,
			Title:          "Read Memory",
		}),
		tools.BuiltinTool(
			"memory_write",
			"Write a value to the agent's persistent memory. Overwrites existing content.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{
						"type":        "string",
						"description": "The memory key to write",
					},
					"value": map[string]any{
						"type":        "string",
						"description": "The content to store",
					},
				},
				"required": []string{"key", "value"},
			},
			m.handleWrite,
		).WithAnnotations(&tools.Annotations{
			DestructiveHint: nonDestructive,
			IdempotentHint:  true,
			OpenWorldHint:   closedWorld,
			Title:           "Write Memory",
		}),
		tools.BuiltinTool(
			"memory_list",
			"List all keys in the agent's persistent memory",
			map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			m.handleList,
		).WithAnnotations(&tools.Annotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  closedWorld,
			Title:          "List Memory Keys",
		}),
		tools.BuiltinTool(
			"memory_delete",
			"Delete a key from the agent's persistent memory",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{
						"type":        "string",
						"description": "The memory key to delete",
					},
				},
				"required": []string{"key"},
			},
			m.handleDelete,
		).WithAnnotations(&tools.Annotations{
			OpenWorldHint: closedWorld,
			Title:         "Delete Memory Key",
		}),
		tools.BuiltinTool(
			"memory_search",
			"Search for a substring across all memory values",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Substring to search for across all memory values",
					},
				},
				"required": []string{"pattern"},
			},
			m.handleSearch,
		).WithAnnotations(&tools.Annotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  closedWorld,
			Title:          "Search Memory",
		}),
	}
}

func (m *Manager) handleRead(input map[string]any) (string, error) {
	key, ok := input["key"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: key")
	}
	return m.Get(key)
}

func (m *Manager) handleWrite(input map[string]any) (string, error) {
	key, ok := input["key"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: key")
	}
	value, ok := input["value"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: value")
	}
	if err := m.Set(key, value); err != nil {
		return "", err
	}
	return fmt.Sprintf("Stored %d bytes under key %q", len(value), key), nil
}

func (m *Manager) handleList(input map[string]any) (string, error) {
	keys, err := m.Keys()
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "No keys in memory.", nil
	}
	data, _ := json.Marshal(keys)
	return "Keys: " + string(data), nil
}

func (m *Manager) handleDelete(input map[string]any) (string, error) {
	key, ok := input["key"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: key")
	}
	if err := m.Delete(key); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted key %q", key), nil
}

func (m *Manager) handleSearch(input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: pattern")
	}
	results, err := m.Search(pattern)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No matches found.", nil
	}
	data, _ := json.Marshal(results)
	return string(data), nil
}

// FormatKeysAsContext returns a summary of all memory keys for inclusion
// in system prompts. Returns empty string if no keys exist.
func (m *Manager) FormatKeysAsContext() string {
	keys, err := m.Keys()
	if err != nil || len(keys) == 0 {
		return ""
	}
	return "Available memory keys: " + strings.Join(keys, ", ")
}
