package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Access distinguishes reads from writes; read-only roots admit only Read.
type Access int

const (
	Read Access = iota
	Write
)

// Sandbox is the resolved, immutable runtime form of a Config.
type Sandbox struct {
	cfg      Config
	cwd      string
	dirs     []string // resolved read-write roots
	readOnly []string // resolved read-only roots
	rules    []AllowRule
	warnings []string
}

// New resolves cfg against cwd (which must be absolute). readOnlyDirs are
// extra roots readable but not writable (the agent's spill directory);
// they may not exist yet. Symlinks in every root are evaluated once, here.
func New(cfg Config, cwd string, readOnlyDirs ...string) (*Sandbox, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("sandbox: working directory %q is not absolute", cwd)
	}
	n := cfg.Normalize()
	s := &Sandbox{cfg: n}

	resolvedCwd, err := resolveLenient(cwd)
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolving working directory: %w", err)
	}
	s.cwd = resolvedCwd

	home := ""
	if h, err := os.UserHomeDir(); err == nil {
		home, _ = resolveLenient(h)
	}
	for _, entry := range n.AllowedDirs {
		abs := entry
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(s.cwd, abs)
		}
		resolved, err := resolveLenient(abs)
		if err != nil {
			return nil, fmt.Errorf("sandbox.allowed_dirs entry %q: %w", entry, err)
		}
		switch {
		case isRoot(resolved):
			s.warnings = append(s.warnings, fmt.Sprintf("sandbox.allowed_dirs entry %q resolves to / — file tools can reach the whole filesystem", entry))
		case home != "" && resolved == home:
			s.warnings = append(s.warnings, fmt.Sprintf("sandbox.allowed_dirs entry %q resolves to your home directory — file tools can reach every file you own", entry))
		}
		if _, err := os.Stat(resolved); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("sandbox.allowed_dirs entry %q (%s) does not exist", entry, resolved))
		}
		s.dirs = append(s.dirs, resolved)
	}
	for _, d := range readOnlyDirs {
		if d == "" {
			continue
		}
		if resolved, err := resolveLenient(d); err == nil {
			s.readOnly = append(s.readOnly, resolved)
		}
	}
	for _, e := range n.AllowCommands {
		r, _ := ParseAllowEntry(e) // validated above
		s.rules = append(s.rules, r)
	}
	if n.Bash == BashUnrestricted {
		s.warnings = append(s.warnings, "sandbox.bash is unrestricted — run_command runs any shell command with your user's permissions")
	}
	return s, nil
}

// Resolve returns the absolute, symlink-free form of path if it lies inside
// an allowed directory (or, for Read, a read-only root). Relative paths
// resolve against the sandbox working directory. Tools must operate on the
// returned path, never the original.
func (s *Sandbox) Resolve(path string, access Access) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.cwd, abs)
	}
	resolved, err := resolveLenient(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", path, err)
	}
	for _, d := range s.dirs {
		if within(d, resolved) {
			return resolved, nil
		}
	}
	if access == Read {
		for _, d := range s.readOnly {
			if within(d, resolved) {
				return resolved, nil
			}
		}
	}
	return "", fmt.Errorf("path %q is outside the allowed directories (%s); use a path inside them, or ask the user to extend sandbox.allowed_dirs",
		path, strings.Join(s.dirs, ", "))
}

// CheckCommand parses command and returns its argv if an allow_commands
// rule permits it. Only meaningful in restricted mode.
func (s *Sandbox) CheckCommand(command string) ([]string, error) {
	if len(s.rules) == 0 {
		return nil, fmt.Errorf("run_command is disabled for this agent: sandbox.allow_commands is empty. " +
			`Ask the user to allow specific commands (for example: <agent> config set sandbox.allow_commands "go test *"), or to set sandbox.bash: unrestricted`)
	}
	argv, err := SplitArgv(command)
	if err != nil {
		return nil, fmt.Errorf("command rejected: %w (run_command has no shell in restricted mode: pipes, redirects, chaining and substitution are unavailable)", err)
	}
	for _, r := range s.rules {
		if r.Matches(argv) {
			return argv, nil
		}
	}
	return nil, fmt.Errorf("command %q is not allowed; sandbox.allow_commands permits: %s", command, strings.Join(s.cfg.AllowCommands, "; "))
}

// Config returns the normalized configuration (a copy).
func (s *Sandbox) Config() Config { return s.cfg.Normalize() }

// Cwd returns the resolved working directory relative paths are joined to.
func (s *Sandbox) Cwd() string { return s.cwd }

// AllowedDirs returns the resolved read-write roots.
func (s *Sandbox) AllowedDirs() []string { return append([]string(nil), s.dirs...) }

// Warnings returns startup warnings (/, $HOME, missing dirs, unrestricted).
func (s *Sandbox) Warnings() []string { return append([]string(nil), s.warnings...) }
