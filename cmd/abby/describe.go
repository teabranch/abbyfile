package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// agentManifest is the JSON output from --describe.
type agentManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Memory      bool   `json:"memory"`
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
