package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/teabranch/agentfile/pkg/fsutil"
	"gopkg.in/yaml.v3"
)

// Load reads config.yaml for the named agent from ~/.agentfile/<name>/config.yaml.
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

// Path returns ~/.agentfile/<name>/config.yaml, or "" if HOME cannot be resolved.
func Path(agentName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agentfile", agentName, "config.yaml")
}

// Write writes a Config to ~/.agentfile/<name>/config.yaml atomically.
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
	default:
		return fmt.Errorf("unsupported config field: %s (use Write for complex fields)", field)
	}

	return WriteTo(path, cfg)
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
