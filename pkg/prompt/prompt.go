// Package prompt handles loading agent system prompts from embedded filesystems
// with support for local override files during development.
package prompt

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Loader loads system prompts from an embedded filesystem, with optional
// override from a local file at ~/.abbyfile/<agent-name>/override.md.
type Loader struct {
	agentName string
	embedFS   embed.FS
	embedPath string
}

// NewLoader creates a Loader that reads from the given embedded filesystem path,
// falling back to override.md if present.
func NewLoader(agentName string, fs embed.FS, path string) *Loader {
	return &Loader{
		agentName: agentName,
		embedFS:   fs,
		embedPath: path,
	}
}

// Load returns the system prompt. If an override file exists, it takes precedence
// over the embedded prompt.
func (l *Loader) Load() (string, error) {
	if l.IsOverridden() {
		data, err := os.ReadFile(l.OverridePath())
		if err != nil {
			return "", fmt.Errorf("reading override file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	data, err := l.embedFS.ReadFile(l.embedPath)
	if err != nil {
		return "", fmt.Errorf("reading embedded prompt %q: %w", l.embedPath, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// IsOverridden returns true if a local override file exists.
func (l *Loader) IsOverridden() bool {
	_, err := os.Stat(l.OverridePath())
	return err == nil
}

// OverridePath returns the filesystem path where an override file would be located.
func (l *Loader) OverridePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".abbyfile", l.agentName, "override.md")
}
