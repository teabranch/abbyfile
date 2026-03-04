package definition

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// CustomToolDef describes a custom CLI tool declared in agent frontmatter.
type CustomToolDef struct {
	Name        string         `yaml:"name"`
	Command     string         `yaml:"command"`
	Description string         `yaml:"description"`
	Args        []string       `yaml:"args"`
	InputSchema map[string]any `yaml:"input_schema"`
}

// AgentDef is the parsed definition of a single agent, combining
// data from the Agentfile reference and the agent's .md file.
type AgentDef struct {
	Name        string
	Description string
	Tools       []string // Claude Code tool names: "Read", "Write", etc.
	CustomTools []CustomToolDef
	Memory      bool
	Version     string // set from Agentfile, not the .md
	PromptBody  string // markdown after frontmatter
}

// frontmatter block 1: agent identity for Claude Code (name, memory).
type frontmatter1 struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Model       string `yaml:"model"`
	Memory      string `yaml:"memory"`
}

// frontmatter block 2: detailed metadata (tools, full description).
type frontmatter2 struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Tools       string          `yaml:"tools"`
	CustomTools []CustomToolDef `yaml:"custom_tools"`
	Model       string          `yaml:"model"`
}

// ParseAgentMD reads an agent .md file with dual frontmatter blocks.
//
// Format:
//
//	---
//	name: go-pro
//	memory: project
//	---
//
//	---
//	description: "Full description"
//	tools: Read, Write, Bash
//	---
//
//	Prompt body here...
func ParseAgentMD(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading agent file: %w", err)
	}

	block1Str, block2Str, body, err := parseDualFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing frontmatter in %s: %w", path, err)
	}

	var fm1 frontmatter1
	if err := yaml.Unmarshal([]byte(block1Str), &fm1); err != nil {
		return nil, fmt.Errorf("parsing first frontmatter block: %w", err)
	}

	var fm2 frontmatter2
	if err := yaml.Unmarshal([]byte(block2Str), &fm2); err != nil {
		return nil, fmt.Errorf("parsing second frontmatter block: %w", err)
	}

	def := &AgentDef{
		Name:       fm1.Name,
		Memory:     fm1.Memory != "",
		PromptBody: body,
	}

	// Description: prefer block 2's (more detailed), fall back to block 1.
	if fm2.Description != "" {
		def.Description = fm2.Description
	} else {
		def.Description = fm1.Description
	}

	// Parse comma-separated tool names.
	if fm2.Tools != "" {
		for _, t := range strings.Split(fm2.Tools, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				def.Tools = append(def.Tools, t)
			}
		}
	}

	// Validate and assign custom tools.
	for i, ct := range fm2.CustomTools {
		if ct.Name == "" {
			return nil, fmt.Errorf("custom_tools[%d]: name is required", i)
		}
		if ct.Command == "" {
			return nil, fmt.Errorf("custom_tools[%d] (%s): command is required", i, ct.Name)
		}
	}
	def.CustomTools = fm2.CustomTools

	return def, nil
}

// parseDualFrontmatter splits content with two --- delimited blocks.
// Returns the YAML text of each block and the body after the second block.
func parseDualFrontmatter(content string) (block1, block2, body string, err error) {
	lines := strings.Split(content, "\n")

	var delimIndices []int
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			delimIndices = append(delimIndices, i)
		}
	}

	if len(delimIndices) < 4 {
		return "", "", "", fmt.Errorf("expected at least 4 '---' delimiters, found %d", len(delimIndices))
	}

	// Block 1: between delimiters 0 and 1.
	block1 = strings.Join(lines[delimIndices[0]+1:delimIndices[1]], "\n")

	// Block 2: between delimiters 2 and 3.
	block2 = strings.Join(lines[delimIndices[2]+1:delimIndices[3]], "\n")

	// Body: everything after delimiter 3.
	body = strings.TrimSpace(strings.Join(lines[delimIndices[3]+1:], "\n"))

	return block1, block2, body, nil
}
