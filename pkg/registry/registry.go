// Package registry tracks installed agent binaries with their source,
// version, and installation scope.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry describes an installed agent.
type Entry struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // "local" or "github.com/owner/repo/agent"
	Version     string `json:"version"`
	Path        string `json:"path"`                  // absolute path to installed binary
	Scope       string `json:"scope"`                 // "local" or "global"
	InstalledAt string `json:"installedAt,omitempty"` // RFC3339 timestamp
}

// Registry is a collection of installed agent entries.
type Registry struct {
	Agents map[string]Entry `json:"agents"`
	path   string           // file path for persistence
}

// DefaultPath returns the default registry file path (~/.agentfile/registry.json).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, ".agentfile", "registry.json"), nil
}

// Load reads a registry from disk. Returns an empty registry if the file
// does not exist.
func Load(path string) (*Registry, error) {
	r := &Registry{
		Agents: make(map[string]Entry),
		path:   path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, fmt.Errorf("reading registry: %w", err)
	}

	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("parsing registry: %w", err)
	}
	if r.Agents == nil {
		r.Agents = make(map[string]Entry)
	}
	return r, nil
}

// Save writes the registry to disk atomically (write temp + rename).
func (r *Registry) Save() error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating registry dir: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}
	data = append(data, '\n')

	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp registry: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming registry: %w", err)
	}
	return nil
}

// Set adds or updates an agent entry, setting InstalledAt to now.
func (r *Registry) Set(e Entry) {
	e.InstalledAt = time.Now().UTC().Format(time.RFC3339)
	r.Agents[e.Name] = e
}

// Get returns an entry by name and whether it exists.
func (r *Registry) Get(name string) (Entry, bool) {
	e, ok := r.Agents[name]
	return e, ok
}

// Remove deletes an entry by name.
func (r *Registry) Remove(name string) {
	delete(r.Agents, name)
}

// List returns all entries.
func (r *Registry) List() []Entry {
	entries := make([]Entry, 0, len(r.Agents))
	for _, e := range r.Agents {
		entries = append(entries, e)
	}
	return entries
}
