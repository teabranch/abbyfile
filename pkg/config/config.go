// Package config handles runtime settings overrides for abbyfile binaries.
// Each agent can have an optional config.yaml at ~/.abbyfile/<name>/config.yaml
// that overrides compiled-in defaults without rebuilding.
package config

// Config represents runtime overrides from ~/.abbyfile/<name>/config.yaml.
// Nil pointer fields mean "not specified" — the compiled default is kept.
type Config struct {
	Model         *string                `yaml:"model,omitempty"`
	ToolTimeout   *string                `yaml:"tool_timeout,omitempty"` // duration string, e.g. "60s"
	MemoryLimits  *MemoryLimitsOverride  `yaml:"memory_limits,omitempty"`
	CommandPolicy *CommandPolicyOverride `yaml:"command_policy,omitempty"`
	ContextBudget *ContextBudgetOverride `yaml:"context_budget,omitempty"`
	Sandbox       *SandboxOverride       `yaml:"sandbox,omitempty"`
}

// SandboxOverride holds optional overrides for the sandbox: block.
type SandboxOverride struct {
	AllowedDirs       *[]string `yaml:"allowed_dirs,omitempty"`
	Bash              *string   `yaml:"bash,omitempty"`
	AllowCommands     *[]string `yaml:"allow_commands,omitempty"`
	MaxCommandTimeout *string   `yaml:"max_command_timeout,omitempty"` // duration string, e.g. "120s"
}

// MemoryLimitsOverride holds optional overrides for memory capacity limits.
type MemoryLimitsOverride struct {
	MaxKeys       *int    `yaml:"max_keys,omitempty"`
	MaxValueBytes *int64  `yaml:"max_value_bytes,omitempty"`
	MaxTotalBytes *int64  `yaml:"max_total_bytes,omitempty"`
	TTL           *string `yaml:"ttl,omitempty"` // duration string, e.g. "72h"
}

// CommandPolicyOverride holds optional overrides for command execution policy.
type CommandPolicyOverride struct {
	AllowedPrefixes  *[]string `yaml:"allowed_prefixes,omitempty"`
	DeniedSubstrings *[]string `yaml:"denied_substrings,omitempty"`
	MaxOutputBytes   *int64    `yaml:"max_output_bytes,omitempty"`
}

// ContextBudgetOverride holds optional overrides for context budget limits.
type ContextBudgetOverride struct {
	MaxOutputLines    *int    `yaml:"max_output_lines,omitempty"`
	MaxOutputBytes    *int64  `yaml:"max_output_bytes,omitempty"`
	OnOverflow        *string `yaml:"on_overflow,omitempty"`
	HeadLines         *int    `yaml:"head_lines,omitempty"`
	TailLines         *int    `yaml:"tail_lines,omitempty"`
	SummaryLines      *int    `yaml:"summary_lines,omitempty"`
	EagerInstructions *bool   `yaml:"eager_instructions,omitempty"`
}

// IsZero returns true if no fields are set (all nil).
func (c *Config) IsZero() bool {
	return c.Model == nil && c.ToolTimeout == nil && c.MemoryLimits == nil && c.CommandPolicy == nil && c.ContextBudget == nil && c.Sandbox == nil
}
