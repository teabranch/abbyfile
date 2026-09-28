package sandbox

import (
	"fmt"
	"strings"
	"time"
)

// BashMode selects how run_command executes.
type BashMode string

const (
	// BashRestricted runs allowlisted argv directly, with no shell.
	BashRestricted BashMode = "restricted"
	// BashUnrestricted runs the command string through sh -c.
	BashUnrestricted BashMode = "unrestricted"
)

// DefaultMaxCommandTimeout caps the timeout the model may request for run_command.
const DefaultMaxCommandTimeout = 120 * time.Second

// Config is the sandbox: frontmatter block and its config.yaml overrides.
type Config struct {
	AllowedDirs       []string // relative entries resolve against the working directory; ["/"] opts out
	Bash              BashMode
	AllowCommands     []string
	MaxCommandTimeout time.Duration
}

// Default returns the secure defaults: the working directory only,
// restricted mode, and no allowed commands.
func Default() Config {
	return Config{
		AllowedDirs:       []string{"."},
		Bash:              BashRestricted,
		MaxCommandTimeout: DefaultMaxCommandTimeout,
	}
}

// Normalize fills zero-valued fields with defaults and returns a copy.
// A nil AllowedDirs means ["."]; an explicit empty slice is kept so that
// Validate can reject it.
func (c Config) Normalize() Config {
	out := Config{Bash: c.Bash, MaxCommandTimeout: c.MaxCommandTimeout}
	if c.AllowedDirs == nil {
		out.AllowedDirs = []string{"."}
	} else {
		out.AllowedDirs = append(make([]string, 0, len(c.AllowedDirs)), c.AllowedDirs...)
	}
	out.AllowCommands = append(make([]string, 0, len(c.AllowCommands)), c.AllowCommands...)
	if out.Bash == "" {
		out.Bash = BashRestricted
	}
	if out.MaxCommandTimeout == 0 {
		out.MaxCommandTimeout = DefaultMaxCommandTimeout
	}
	return out
}

// Validate reports the first illegal value, naming the sandbox key.
func (c Config) Validate() error {
	n := c.Normalize()
	if len(n.AllowedDirs) == 0 {
		return fmt.Errorf(`sandbox.allowed_dirs must list at least one directory (use ["."] for the working directory)`)
	}
	for _, d := range n.AllowedDirs {
		if strings.TrimSpace(d) == "" {
			return fmt.Errorf("sandbox.allowed_dirs: entries must not be blank")
		}
	}
	switch n.Bash {
	case BashRestricted, BashUnrestricted:
	default:
		return fmt.Errorf("sandbox.bash: invalid value %q (want restricted or unrestricted)", c.Bash)
	}
	if n.MaxCommandTimeout < 0 {
		return fmt.Errorf("sandbox.max_command_timeout must be positive, got %s", n.MaxCommandTimeout)
	}
	for _, e := range n.AllowCommands {
		if _, err := ParseAllowEntry(e); err != nil {
			return fmt.Errorf("sandbox.%w", err)
		}
	}
	return nil
}
