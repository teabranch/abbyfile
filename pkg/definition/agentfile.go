// Package definition parses Agentfile manifests and agent .md files
// into structured definitions used by the builder.
package definition

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Agentfile is the top-level manifest declaring which agents to build.
type Agentfile struct {
	Version string              `yaml:"version"`
	Agents  map[string]AgentRef `yaml:"agents"`
}

// AgentRef points to an agent's .md file and its version.
type AgentRef struct {
	Path    string `yaml:"path"`
	Version string `yaml:"version"`
}

// ParseAgentfile reads and validates an Agentfile YAML manifest.
func ParseAgentfile(path string) (*Agentfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading agentfile: %w", err)
	}

	var af Agentfile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("parsing agentfile: %w", err)
	}

	if af.Version == "" {
		return nil, fmt.Errorf("agentfile: version is required")
	}
	if len(af.Agents) == 0 {
		return nil, fmt.Errorf("agentfile: at least one agent is required")
	}
	for name, ref := range af.Agents {
		if ref.Path == "" {
			return nil, fmt.Errorf("agent %q: path is required", name)
		}
		if ref.Version == "" {
			return nil, fmt.Errorf("agent %q: version is required", name)
		}
	}

	return &af, nil
}
