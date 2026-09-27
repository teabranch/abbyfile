package definition

import (
	"fmt"
	"time"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// SandboxDef is the parsed sandbox: frontmatter block.
type SandboxDef struct {
	AllowedDirs       []string `yaml:"allowed_dirs"`
	Bash              string   `yaml:"bash"`
	AllowCommands     []string `yaml:"allow_commands"`
	MaxCommandTimeout string   `yaml:"max_command_timeout"` // Go duration, e.g. "120s"
}

// ToConfig validates the block and returns the normalized sandbox.Config.
func (s *SandboxDef) ToConfig() (sandbox.Config, error) {
	cfg := sandbox.Config{
		AllowedDirs:   s.AllowedDirs,
		Bash:          sandbox.BashMode(s.Bash),
		AllowCommands: s.AllowCommands,
	}
	if s.MaxCommandTimeout != "" {
		d, err := time.ParseDuration(s.MaxCommandTimeout)
		if err != nil {
			return sandbox.Config{}, fmt.Errorf("sandbox.max_command_timeout: %w", err)
		}
		if d <= 0 {
			return sandbox.Config{}, fmt.Errorf("sandbox.max_command_timeout must be positive, got %s", s.MaxCommandTimeout)
		}
		cfg.MaxCommandTimeout = d
	}
	if err := cfg.Validate(); err != nil {
		return sandbox.Config{}, err
	}
	return cfg.Normalize(), nil
}

func validateSandbox(s *SandboxDef) error {
	if s == nil {
		return nil
	}
	_, err := s.ToConfig()
	return err
}
