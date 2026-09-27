package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/teabranch/abbyfile/pkg/fsutil"
	"github.com/teabranch/abbyfile/pkg/sandbox"
	"gopkg.in/yaml.v3"
)

// Load reads config.yaml for the named agent from ~/.abbyfile/<name>/config.yaml.
// Returns a zero Config (all nil fields) if the file does not exist.
func Load(agentName string) (*Config, error) {
	p := Path(agentName)
	if p == "" {
		return &Config{}, nil
	}
	return LoadFrom(p)
}

// LoadFrom reads config from a specific file path.
// Returns a zero Config if the file does not exist.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return &cfg, nil
}

// Path returns ~/.abbyfile/<name>/config.yaml, or "" if HOME cannot be resolved.
func Path(agentName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".abbyfile", agentName, "config.yaml")
}

// Write writes a Config to ~/.abbyfile/<name>/config.yaml atomically.
// Only non-nil fields are written.
func Write(agentName string, cfg *Config) error {
	p := Path(agentName)
	if p == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	return WriteTo(p, cfg)
}

// WriteTo writes a Config to a specific path atomically.
func WriteTo(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return fsutil.WriteAtomic(path, data, 0o600)
}

// WriteField writes a single field to config.yaml, merging with any existing config.
func WriteField(agentName, field, value string) error {
	p := Path(agentName)
	if p == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	return WriteFieldTo(p, field, value)
}

// WriteFieldTo writes a single field to a specific config path, merging with existing.
func WriteFieldTo(path, field, value string) error {
	cfg, err := LoadFrom(path)
	if err != nil {
		return err
	}

	switch field {
	case "model":
		cfg.Model = &value
	case "tool_timeout":
		cfg.ToolTimeout = &value
	case "context_budget.max_output_lines":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("max_output_lines must be an integer: %w", err)
		}
		ensureBudget(cfg)
		cfg.ContextBudget.MaxOutputLines = &n
	case "context_budget.max_output_bytes":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("max_output_bytes must be an integer: %w", err)
		}
		ensureBudget(cfg)
		cfg.ContextBudget.MaxOutputBytes = &n
	case "context_budget.on_overflow":
		switch value {
		case "head-tail", "spill", "passthrough":
		default:
			return fmt.Errorf("on_overflow must be head-tail, spill, or passthrough")
		}
		ensureBudget(cfg)
		v := value
		cfg.ContextBudget.OnOverflow = &v
	case "context_budget.head_lines":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("head_lines must be an integer: %w", err)
		}
		ensureBudget(cfg)
		cfg.ContextBudget.HeadLines = &n
	case "context_budget.tail_lines":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("tail_lines must be an integer: %w", err)
		}
		ensureBudget(cfg)
		cfg.ContextBudget.TailLines = &n
	case "context_budget.summary_lines":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("summary_lines must be an integer: %w", err)
		}
		ensureBudget(cfg)
		cfg.ContextBudget.SummaryLines = &n
	case "context_budget.eager_instructions":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("eager_instructions must be true or false: %w", err)
		}
		ensureBudget(cfg)
		cfg.ContextBudget.EagerInstructions = &b
	case "sandbox.allowed_dirs":
		dirs, err := ParseList(value)
		if err != nil {
			return fmt.Errorf("sandbox.allowed_dirs: %w", err)
		}
		if len(dirs) == 0 {
			return fmt.Errorf(`sandbox.allowed_dirs needs at least one directory (use "." for the working directory)`)
		}
		ensureSandbox(cfg)
		cfg.Sandbox.AllowedDirs = &dirs
	case "sandbox.bash":
		if value != string(sandbox.BashRestricted) && value != string(sandbox.BashUnrestricted) {
			return fmt.Errorf("sandbox.bash must be restricted or unrestricted")
		}
		ensureSandbox(cfg)
		v := value
		cfg.Sandbox.Bash = &v
	case "sandbox.allow_commands":
		cmds, err := ParseList(value)
		if err != nil {
			return fmt.Errorf("sandbox.allow_commands: %w", err)
		}
		for _, c := range cmds {
			if _, err := sandbox.ParseAllowEntry(c); err != nil {
				return fmt.Errorf("sandbox.%w", err)
			}
		}
		ensureSandbox(cfg)
		cfg.Sandbox.AllowCommands = &cmds
	case "sandbox.max_command_timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("sandbox.max_command_timeout must be a positive duration such as 120s")
		}
		ensureSandbox(cfg)
		v := value
		cfg.Sandbox.MaxCommandTimeout = &v
	default:
		return fmt.Errorf("unsupported config field: %s (use Write for complex fields)", field)
	}

	// Validate the merged sandbox override as a whole (not just the field
	// just set): individual case checks above catch obviously-bad values
	// for that one field, but only this catches a combination that is
	// invalid together, or a sibling field left invalid by a hand-edited
	// config.yaml from before this validation existed. Without it, a value
	// like `sandbox.allowed_dirs '[""]'` could merge into a config that
	// locks every future invocation out of the sandbox (see
	// Agent.effectiveSandbox for the runtime fallback of last resort).
	if strings.HasPrefix(field, "sandbox.") {
		if err := validateSandboxOverride(cfg.Sandbox); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}

	return WriteTo(path, cfg)
}

// validateSandboxOverride builds a sandbox.Config from so's non-nil fields
// layered on sandbox.Default() and validates it.
func validateSandboxOverride(so *SandboxOverride) error {
	cfg := sandbox.Default()
	if so == nil {
		return cfg.Validate()
	}
	if so.AllowedDirs != nil {
		cfg.AllowedDirs = *so.AllowedDirs
	}
	if so.Bash != nil {
		cfg.Bash = sandbox.BashMode(*so.Bash)
	}
	if so.AllowCommands != nil {
		cfg.AllowCommands = *so.AllowCommands
	}
	if so.MaxCommandTimeout != nil {
		if d, err := time.ParseDuration(*so.MaxCommandTimeout); err == nil {
			cfg.MaxCommandTimeout = d
		}
	}
	return cfg.Validate()
}

// ResetField removes a single field from the agent's config.yaml, reverting to the compiled default.
// If all fields become nil, the config file is deleted.
func ResetField(agentName, field string) error {
	p := Path(agentName)
	if p == "" {
		return fmt.Errorf("cannot resolve home directory")
	}
	return ResetFieldTo(p, field)
}

// ResetFieldTo removes a single field from a specific config path.
// If all fields become nil after reset, the config file is deleted.
func ResetFieldTo(path, field string) error {
	cfg, err := LoadFrom(path)
	if err != nil {
		return err
	}

	switch field {
	case "model":
		cfg.Model = nil
	case "tool_timeout":
		cfg.ToolTimeout = nil
	case "memory_limits":
		cfg.MemoryLimits = nil
	case "command_policy":
		cfg.CommandPolicy = nil
	case "context_budget":
		cfg.ContextBudget = nil
	case "sandbox":
		cfg.Sandbox = nil
	default:
		return fmt.Errorf("unsupported config field: %s", field)
	}

	if cfg.IsZero() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing empty config file: %w", err)
		}
		return nil
	}

	return WriteTo(path, cfg)
}

// ensureBudget lazily creates cfg.ContextBudget if it is nil, preserving
// any fields already set so callers can set one sub-field at a time
// without clobbering the others.
func ensureBudget(cfg *Config) {
	if cfg.ContextBudget == nil {
		cfg.ContextBudget = &ContextBudgetOverride{}
	}
}

// ParseList parses a config-set list value: a JSON array (use it when an
// entry contains a comma) or a comma-separated list. Blank (whitespace-only)
// items are dropped in both forms; an empty string yields an empty, non-nil
// list.
func ParseList(value string) ([]string, error) {
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "[") {
		var raw []string
		if err := json.Unmarshal([]byte(v), &raw); err != nil {
			return nil, fmt.Errorf("invalid JSON list: %w", err)
		}
		out := []string{}
		for _, s := range raw {
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// ensureSandbox lazily creates cfg.Sandbox, preserving existing fields.
func ensureSandbox(cfg *Config) {
	if cfg.Sandbox == nil {
		cfg.Sandbox = &SandboxOverride{}
	}
}
