// Package definition parses Abbyfile manifests and agent .md files
// into structured definitions used by the builder.
package definition

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// validAgentName matches safe agent names (alphanumeric, hyphens, underscores).
var validAgentName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// Abbyfile is the top-level manifest declaring which agents to build.
type Abbyfile struct {
	Version string              `yaml:"version"`
	Agents  map[string]AgentRef `yaml:"agents"`
	Publish *PublishConfig      `yaml:"publish,omitempty"`
}

// PublishConfig holds cross-compilation target configuration.
type PublishConfig struct {
	Targets []PublishTarget `yaml:"targets,omitempty"`
}

// PublishTarget specifies an OS/architecture pair for cross-compilation.
type PublishTarget struct {
	OS   string `yaml:"os"`
	Arch string `yaml:"arch"`
}

// AgentRef points to an agent's .md file and its version.
type AgentRef struct {
	Path         string   `yaml:"path"`
	Version      string   `yaml:"version"`
	Dependencies []string `yaml:"dependencies,omitempty"` // other agent names in this Abbyfile
}

// ParseAbbyfile reads and validates an Abbyfile YAML manifest.
func ParseAbbyfile(path string) (*Abbyfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading abbyfile: %w", err)
	}

	var af Abbyfile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("parsing abbyfile: %w", err)
	}

	if af.Version == "" {
		return nil, fmt.Errorf("abbyfile: version is required")
	}
	if af.Version != "1" {
		return nil, fmt.Errorf("abbyfile: unsupported version %q (only \"1\" is supported)", af.Version)
	}
	if len(af.Agents) == 0 {
		return nil, fmt.Errorf("abbyfile: at least one agent is required")
	}
	for name, ref := range af.Agents {
		if !validAgentName.MatchString(name) {
			return nil, fmt.Errorf("agent %q: name contains invalid characters (only alphanumeric, hyphens, underscores allowed)", name)
		}
		if ref.Path == "" {
			return nil, fmt.Errorf("agent %q: path is required", name)
		}
		if ref.Version == "" {
			return nil, fmt.Errorf("agent %q: version is required", name)
		}
	}

	// Validate dependencies reference existing agent names.
	for name, ref := range af.Agents {
		for _, dep := range ref.Dependencies {
			if _, exists := af.Agents[dep]; !exists {
				return nil, fmt.Errorf("agent %q: dependency %q not found in agents", name, dep)
			}
			if dep == name {
				return nil, fmt.Errorf("agent %q: cannot depend on itself", name)
			}
		}
	}

	return &af, nil
}
