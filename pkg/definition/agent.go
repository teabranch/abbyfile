package definition

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// validName matches safe agent, tool, and skill names (alphanumeric, hyphens, underscores).
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// CustomToolDef describes a custom CLI tool declared in agent frontmatter.
type CustomToolDef struct {
	Name        string         `yaml:"name"`
	Command     string         `yaml:"command"`
	Description string         `yaml:"description"`
	Args        []string       `yaml:"args"`
	InputSchema map[string]any `yaml:"input_schema"`
}

// SkillDef describes a skill declared in agent frontmatter for plugin output.
type SkillDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Path        string `yaml:"path"` // relative to agent .md file
}

// AgentDef is the parsed definition of a single agent, combining
// data from the Abbyfile reference and the agent's .md file.
type AgentDef struct {
	Name        string
	Description string
	Tools       []string // Claude Code tool names: "Read", "Write", etc.
	CustomTools []CustomToolDef
	Skills      []SkillDef
	Memory      bool
	Version     string // set from Abbyfile, not the .md
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
	Skills      []SkillDef      `yaml:"skills"`
	Model       string          `yaml:"model"`
}

// singleFrontmatter is the alternative single-block frontmatter format.
// All agent metadata lives under an `abbyfile:` key alongside name/description.
type singleFrontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Model       string         `yaml:"model"`
	Abbyfile    *abbyfileBlock `yaml:"abbyfile"`
}

// abbyfileBlock holds tool, memory, and skill configuration in single-frontmatter format.
type abbyfileBlock struct {
	Tools       []string        `yaml:"tools"`
	Memory      string          `yaml:"memory"`
	CustomTools []CustomToolDef `yaml:"custom_tools"`
	Skills      []SkillDef      `yaml:"skills"`
}

// ParseAgentMD reads an agent .md file with dual or single frontmatter blocks.
//
// Dual format (4 delimiters):
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
//
// Single format (2 delimiters, with abbyfile: key):
//
//	---
//	name: my-agent
//	description: "Full description"
//	abbyfile:
//	  tools: [Read, Write, Bash]
//	  memory: true
//	---
//
//	Prompt body here...
func ParseAgentMD(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading agent file: %w", err)
	}

	content := string(data)

	// Try dual-frontmatter first.
	block1Str, block2Str, body, dualErr := parseDualFrontmatter(content)
	if dualErr == nil {
		return parseDualFormat(block1Str, block2Str, body, path)
	}

	// Fall back to single-frontmatter.
	fmStr, singleBody, singleErr := parseSingleFrontmatter(content)
	if singleErr != nil {
		// Return the original dual-frontmatter error for backward compatibility.
		return nil, fmt.Errorf("parsing frontmatter in %s: %w", path, dualErr)
	}

	return parseSingleFormat(fmStr, singleBody, path)
}

// parseDualFormat processes the dual-frontmatter format into an AgentDef.
func parseDualFormat(block1Str, block2Str, body, path string) (*AgentDef, error) {
	var fm1 frontmatter1
	if err := yaml.Unmarshal([]byte(block1Str), &fm1); err != nil {
		return nil, fmt.Errorf("parsing first frontmatter block: %w", err)
	}

	var fm2 frontmatter2
	if err := yaml.Unmarshal([]byte(block2Str), &fm2); err != nil {
		return nil, fmt.Errorf("parsing second frontmatter block: %w", err)
	}

	if fm1.Name == "" {
		return nil, fmt.Errorf("agent name is required in first frontmatter block")
	}
	if !validName.MatchString(fm1.Name) {
		return nil, fmt.Errorf("agent name %q contains invalid characters (only alphanumeric, hyphens, underscores allowed)", fm1.Name)
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

	if err := validateCustomTools(fm2.CustomTools); err != nil {
		return nil, err
	}
	def.CustomTools = fm2.CustomTools

	if err := validateSkills(fm2.Skills); err != nil {
		return nil, err
	}
	def.Skills = fm2.Skills

	return def, nil
}

// parseSingleFormat processes the single-frontmatter format into an AgentDef.
func parseSingleFormat(fmStr, body, path string) (*AgentDef, error) {
	var sfm singleFrontmatter
	if err := yaml.Unmarshal([]byte(fmStr), &sfm); err != nil {
		return nil, fmt.Errorf("parsing single frontmatter block in %s: %w", path, err)
	}

	if sfm.Name == "" {
		return nil, fmt.Errorf("agent name is required in frontmatter block")
	}
	if !validName.MatchString(sfm.Name) {
		return nil, fmt.Errorf("agent name %q contains invalid characters (only alphanumeric, hyphens, underscores allowed)", sfm.Name)
	}
	if sfm.Abbyfile == nil {
		return nil, fmt.Errorf("parsing frontmatter in %s: single-frontmatter format requires an 'abbyfile:' key", path)
	}

	def := &AgentDef{
		Name:        sfm.Name,
		Description: sfm.Description,
		PromptBody:  body,
	}

	if sfm.Abbyfile != nil {
		def.Tools = sfm.Abbyfile.Tools
		def.Memory = sfm.Abbyfile.Memory != ""

		if err := validateCustomTools(sfm.Abbyfile.CustomTools); err != nil {
			return nil, err
		}
		def.CustomTools = sfm.Abbyfile.CustomTools

		if err := validateSkills(sfm.Abbyfile.Skills); err != nil {
			return nil, err
		}
		def.Skills = sfm.Abbyfile.Skills
	}

	return def, nil
}

// parseSingleFrontmatter splits content with a single --- delimited block (2 delimiters).
// Returns the YAML text and the body after the block.
func parseSingleFrontmatter(content string) (fmBlock, body string, err error) {
	lines := strings.Split(content, "\n")

	var delimIndices []int
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			delimIndices = append(delimIndices, i)
			if len(delimIndices) == 2 {
				break
			}
		}
	}

	if len(delimIndices) < 2 {
		return "", "", fmt.Errorf("expected at least 2 '---' delimiters, found %d", len(delimIndices))
	}

	fmBlock = strings.Join(lines[delimIndices[0]+1:delimIndices[1]], "\n")
	body = strings.TrimSpace(strings.Join(lines[delimIndices[1]+1:], "\n"))

	return fmBlock, body, nil
}

// validateCustomTools validates a slice of custom tool definitions.
func validateCustomTools(cts []CustomToolDef) error {
	for i, ct := range cts {
		if ct.Name == "" {
			return fmt.Errorf("custom_tools[%d]: name is required", i)
		}
		if !validName.MatchString(ct.Name) {
			return fmt.Errorf("custom_tools[%d]: name %q contains invalid characters", i, ct.Name)
		}
		if ct.Command == "" {
			return fmt.Errorf("custom_tools[%d] (%s): command is required", i, ct.Name)
		}
	}
	return nil
}

// validateSkills validates a slice of skill definitions.
func validateSkills(skills []SkillDef) error {
	for i, s := range skills {
		if s.Name == "" {
			return fmt.Errorf("skills[%d]: name is required", i)
		}
		if s.Description == "" {
			return fmt.Errorf("skills[%d] (%s): description is required", i, s.Name)
		}
		if s.Path == "" {
			return fmt.Errorf("skills[%d] (%s): path is required", i, s.Name)
		}
	}
	return nil
}

// parseDualFrontmatter splits content with two --- delimited blocks.
// Returns the YAML text of each block and the body after the second block.
func parseDualFrontmatter(content string) (block1, block2, body string, err error) {
	lines := strings.Split(content, "\n")

	var delimIndices []int
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			delimIndices = append(delimIndices, i)
			if len(delimIndices) == 4 {
				break
			}
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
