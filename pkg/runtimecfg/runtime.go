// Package runtimecfg registers MCP servers with AI coding runtimes (Claude
// Code, Codex, Gemini CLI). Every edit is planned first — a Change carries a
// preview — and applied through the runtime's own CLI when it can express the
// entry, otherwise through a surgical, backed-up edit of its config file.
package runtimecfg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Runtime identifies an AI coding runtime that supports MCP servers.
type Runtime string

const (
	ClaudeCode Runtime = "claude-code"
	Codex      Runtime = "codex"
	Gemini     Runtime = "gemini"
)

// AllRuntimes returns all supported runtimes in deterministic order.
func AllRuntimes() []Runtime { return []Runtime{ClaudeCode, Codex, Gemini} }

// Parse converts a string to a Runtime.
func Parse(s string) (Runtime, error) {
	for _, r := range AllRuntimes() {
		if string(r) == s {
			return r, nil
		}
	}
	return "", fmt.Errorf("unknown runtime %q (supported: claude-code, codex, gemini)", s)
}

// Scope is where an entry is registered.
type Scope string

const (
	ScopeProject Scope = "project" // project root config (default install, abby build)
	ScopeUser    Scope = "user"    // user-global config (--global)
)

// Method is how a change is applied.
type Method string

const (
	MethodAuto Method = "auto" // CLI when it can express the entry, else file
	MethodCLI  Method = "cli"
	MethodFile Method = "file"
)

// ParseMethod parses --config-method / ABBY_CONFIG_METHOD values.
func ParseMethod(s string) (Method, error) {
	switch Method(s) {
	case MethodAuto, MethodCLI, MethodFile:
		return Method(s), nil
	}
	return "", fmt.Errorf("unknown config method %q (want auto, cli or file)", s)
}

// ServerEntry is the part of an MCP server entry abby owns. Zero values mean
// "leave the existing value alone": nil Env keeps an existing env, "" Cwd and
// 0 Timeout set nothing.
type ServerEntry struct {
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	Timeout time.Duration
}

// Change is one planned edit to one runtime's config.
type Change struct {
	Runtime Runtime
	Scope   Scope
	Method  Method // MethodCLI or MethodFile
	Target  string // config file written (directly, or by the runtime CLI)
	Server  string // MCP server name
	Remove  bool
	Noop    bool     // nothing to do (e.g. removing an absent entry)
	Preview string   // entry diff; for MethodCLI also the commands to run
	Notes   []string // user-facing caveats, e.g. Codex project trust
	apply   func() (string, error)
}

// Apply performs the change and returns the backup it created, if any. A
// MethodFile change runs its apply closure even when Noop was true at plan
// time: the closure re-reads the file and returns early if there is still
// nothing to do, so an entry altered on disk between plan and apply (e.g.
// deleted by another process) is still (re)written. A MethodCLI change is
// skipped outright when Noop, since there is no cheap way to re-check "is
// this still a no-op" without invoking the CLI.
func (c Change) Apply() (backup string, err error) {
	if c.apply == nil {
		return "", nil
	}
	if c.Noop && c.Method != MethodFile {
		return "", nil
	}
	return c.apply()
}

// ConfigWriter plans edits to one runtime's MCP config.
type ConfigWriter interface {
	Runtime() Runtime
	ConfigPath(scope Scope) (string, error)
	Lookup(scope Scope, name string) (ServerEntry, bool, error)
	PlanAdd(scope Scope, name string, e ServerEntry) (Change, error)
	PlanRemove(scope Scope, name string) (Change, error)
}

// CommandRunner runs a runtime CLI (injectable for tests). dir, when
// non-empty, is the working directory the command runs in — project-scope
// calls need this to be Options.ProjectRoot, since claude/gemini resolve
// their own "project scope" from the process's cwd, which need not be the
// project abby was told to act on (e.g. an uninstall/update/doctor run from
// elsewhere). "" means the process's own cwd.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error)

// Options configures writers. Zero values use real PATH lookup, a real
// process runner, MethodAuto and the current directory as project root.
type Options struct {
	Method Method
	// ProjectRoot is absolute; project-scope configs live here. "" means the
	// process's working directory at For/Detect/Resolve time (resolved once,
	// by normalized(), not re-read later), not necessarily the directory the
	// Options value was constructed in.
	ProjectRoot string
	LookPath    func(string) (string, error)
	Run         CommandRunner
}

func (o Options) normalized() Options {
	if o.Method == "" {
		o.Method = MethodAuto
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Run == nil {
		o.Run = runCommand
	}
	if o.ProjectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			o.ProjectRoot = wd
		}
	}
	return o
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// For returns the writer for r.
func For(r Runtime, opts Options) ConfigWriter {
	return newWriter(r, opts.normalized())
}

// Detect returns writers for runtimes whose CLI is on PATH or whose config
// directory exists; Claude Code when none is found.
func Detect(opts Options) []ConfigWriter {
	opts = opts.normalized()
	var ws []ConfigWriter
	for _, r := range AllRuntimes() {
		if detected(r, opts) {
			ws = append(ws, newWriter(r, opts))
		}
	}
	if len(ws) == 0 {
		ws = append(ws, newWriter(ClaudeCode, opts))
	}
	return ws
}

// Resolve maps a --runtime flag ("auto", "all" or a runtime name) to writers.
func Resolve(flag string, opts Options) ([]ConfigWriter, error) {
	switch flag {
	case "auto", "":
		return Detect(opts), nil
	case "all":
		var ws []ConfigWriter
		for _, r := range AllRuntimes() {
			ws = append(ws, For(r, opts))
		}
		return ws, nil
	}
	r, err := Parse(flag)
	if err != nil {
		return nil, err
	}
	return []ConfigWriter{For(r, opts)}, nil
}
