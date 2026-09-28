package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type manifestTool struct {
	Name string `json:"name"`
}

type manifestSandbox struct {
	AllowedDirs       []string `json:"allowedDirs"`
	Bash              string   `json:"bash"`
	AllowCommands     []string `json:"allowCommands"`
	MaxCommandTimeout string   `json:"maxCommandTimeout"`
	Warnings          []string `json:"warnings"`
}

// agentManifest is the JSON output from --describe.
type agentManifest struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Memory      bool             `json:"memory"`
	ToolTimeout string           `json:"toolTimeout"`
	Tools       []manifestTool   `json:"tools"`
	Sandbox     *manifestSandbox `json:"sandbox"`
}

// runtimeTimeoutMargin is added to the agent's largest limit so the runtime
// never kills a call the server would allow (spec B3/C4).
const runtimeTimeoutMargin = 10 * time.Second

const defaultAgentToolTimeout = 30 * time.Second

// runtimeTimeout is the runtime-side timeout for an agent: its largest
// effective tool limit plus a margin; 0 (omit) when m is nil.
func runtimeTimeout(m *agentManifest) time.Duration {
	if m == nil {
		return 0
	}
	limit := defaultAgentToolTimeout
	if d, err := time.ParseDuration(m.ToolTimeout); err == nil && d > 0 {
		limit = d
	}
	if m.Sandbox != nil {
		for _, t := range m.Tools {
			if t.Name == "run_command" {
				if d, err := time.ParseDuration(m.Sandbox.MaxCommandTimeout); err == nil && d > limit {
					limit = d
				}
			}
		}
	}
	return limit + runtimeTimeoutMargin
}

// describeAgent runs a binary with --describe and parses the JSON manifest.
// Used to verify a downloaded binary is a valid agent.
func describeAgent(binaryPath string) (*agentManifest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "--describe")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running --describe on %s: %w", binaryPath, err)
	}

	var m agentManifest
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("parsing --describe output: %w", err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("binary at %s produced empty agent name", binaryPath)
	}
	return &m, nil
}
