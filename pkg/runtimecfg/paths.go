package runtimecfg

import (
	"os"
	"path/filepath"
)

// cliName is the runtime's executable name.
func cliName(r Runtime) string {
	switch r {
	case ClaudeCode:
		return "claude"
	case Codex:
		return "codex"
	default:
		return "gemini"
	}
}

func claudeConfigDir() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func codexHome() (string, error) {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// configPath returns the config file a runtime reads for scope; project
// scope is joined to root (an absolute project directory).
func configPath(r Runtime, scope Scope, root string) (string, error) {
	switch r {
	case ClaudeCode:
		if scope == ScopeProject {
			return filepath.Join(root, ".mcp.json"), nil
		}
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			return filepath.Join(d, ".claude.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude.json"), nil
	case Codex:
		if scope == ScopeProject {
			return filepath.Join(root, ".codex", "config.toml"), nil
		}
		d, err := codexHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "config.toml"), nil
	default:
		if scope == ScopeProject {
			return filepath.Join(root, ".gemini", "settings.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".gemini", "settings.json"), nil
	}
}

// detectionDir is the runtime's own config directory (C3).
func detectionDir(r Runtime) (string, error) {
	switch r {
	case ClaudeCode:
		return claudeConfigDir()
	case Codex:
		return codexHome()
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".gemini"), nil
	}
}

func detected(r Runtime, opts Options) bool {
	if _, err := opts.LookPath(cliName(r)); err == nil {
		return true
	}
	d, err := detectionDir(r)
	if err != nil {
		return false
	}
	fi, err := os.Stat(d)
	return err == nil && fi.IsDir()
}

// LegacyClaudePath is ~/.claude/mcp.json, which abby <= v0.11 wrote for
// Claude Code's user scope; Claude Code never reads it.
func LegacyClaudePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "mcp.json"), nil
}
