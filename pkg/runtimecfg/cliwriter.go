package runtimecfg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teabranch/abbyfile/pkg/fsutil"
)

// cliTimeout bounds one runtime-CLI invocation.
const cliTimeout = 30 * time.Second

// existingJSON returns the current entry for JSON runtimes (nil when absent
// or for Codex). A parse failure is returned so nothing proceeds on a broken file.
func (w *writer) existingJSON(scope Scope, name string) (jsonObject, error) {
	if w.isTOML() {
		return nil, nil
	}
	path, err := w.ConfigPath(scope)
	if err != nil {
		return nil, err
	}
	snap, err := fsutil.ReadSnapshot(path)
	if err != nil {
		return nil, err
	}
	if !snap.Exists {
		return nil, nil
	}
	o, _, err := lookupJSONServer(snap.Data, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w; abby did not modify it", path, err)
	}
	return o, nil
}

// cliReason returns "" when the runtime CLI can apply this change without
// losing anything, else why not. e == nil means a removal.
func (w *writer) cliReason(scope Scope, e *ServerEntry, existing jsonObject) string {
	switch w.r {
	case Codex:
		return "codex's CLI cannot set cwd or timeouts and has no project scope"
	case Gemini:
		if scope != ScopeUser {
			return "gemini's CLI cannot set cwd, which project-scope entries need"
		}
		if e != nil && e.Cwd != "" {
			return "gemini's CLI cannot set cwd"
		}
		if e != nil {
			for _, a := range e.Args {
				if strings.HasPrefix(a, "-") {
					return "an argument starts with '-', which gemini's CLI would parse as a flag"
				}
			}
			// gemini's "mcp add" overwrites the whole entry, so a field abby
			// isn't setting on this change (cwd is always "" here; timeout
			// when e.g. --describe failed) must not already exist on the
			// entry, or the CLI would silently erase it.
			if _, ok := existing.get("cwd"); ok {
				return "gemini's CLI would silently drop the existing cwd"
			}
			if e.Timeout < time.Second {
				if _, ok := existing.get("timeout"); ok {
					return "gemini's CLI would silently drop the existing timeout"
				}
			}
		}
	}
	owned := ownedKeys(w.r)
	for _, f := range existing {
		if !owned[f.Key] {
			return fmt.Sprintf("the existing entry has %q, which %s's CLI would drop", f.Key, cliName(w.r))
		}
	}
	return ""
}

// choose picks MethodCLI or MethodFile for one change.
func (w *writer) choose(scope Scope, e *ServerEntry, existing jsonObject) (Method, error) {
	if w.opts.Method == MethodFile {
		return MethodFile, nil
	}
	_, pathErr := w.opts.LookPath(cliName(w.r))
	reason := w.cliReason(scope, e, existing)
	if w.opts.Method == MethodCLI {
		if pathErr != nil {
			return "", fmt.Errorf("--config-method cli: %s is not on PATH", cliName(w.r))
		}
		if reason != "" {
			return "", fmt.Errorf("--config-method cli: %s", reason)
		}
		return MethodCLI, nil
	}
	if pathErr == nil && reason == "" {
		return MethodCLI, nil
	}
	return MethodFile, nil
}

// alreadyAbsentMsg is what claude's own "mcp remove" prints (to stderr,
// exit 1) when the name isn't registered; abby treats that as success, not
// a failure, since the end state abby wants (no such entry) already holds.
const alreadyAbsentMsg = "No MCP server named"

// execCLI runs the runtime CLI with a bounded timeout. Project-scope calls
// run in Options.ProjectRoot, not the process's own working directory:
// claude/gemini's "mcp" subcommands resolve their own project scope from
// their process's cwd, which need not be the project abby was told to act
// on (Options.ProjectRoot), for example an uninstall/update/doctor run from
// elsewhere. User scope has no such directory dependency and runs in the
// process's own cwd ("").
func (w *writer) execCLI(scope Scope, args ...string) (stdout, stderr []byte, err error) {
	dir := ""
	if scope == ScopeProject {
		dir = w.opts.ProjectRoot
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	return w.opts.Run(ctx, dir, cliName(w.r), args...)
}

// wrapRunErr turns a failed execCLI call into an error naming the command;
// a context timeout is reported specially, and when stderr is empty the
// (trimmed) stdout is used instead, so a CLI that only prints to stdout
// still surfaces a useful message.
func (w *writer) wrapRunErr(err error, stdout, stderr []byte, args []string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s %s timed out after %s", cliName(w.r), strings.Join(args, " "), cliTimeout)
	}
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		msg = strings.TrimSpace(string(stdout))
	}
	return fmt.Errorf("%s %s: %w: %s", cliName(w.r), strings.Join(args, " "), err, msg)
}

func (w *writer) run(scope Scope, args ...string) error {
	stdout, stderr, err := w.execCLI(scope, args...)
	if err == nil {
		return nil
	}
	return w.wrapRunErr(err, stdout, stderr, args)
}

// runRemove is like run but treats "already absent" — the runtime's own
// idempotent response to removing a name it doesn't have — as success, so
// neither a race nor a repeat removal surfaces as an abby error.
func (w *writer) runRemove(scope Scope, args ...string) error {
	stdout, stderr, err := w.execCLI(scope, args...)
	if err == nil {
		return nil
	}
	if strings.Contains(string(stdout), alreadyAbsentMsg) || strings.Contains(string(stderr), alreadyAbsentMsg) {
		return nil
	}
	return w.wrapRunErr(err, stdout, stderr, args)
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\{}[]*?;&|<>()") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func commandLine(name string, args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shellQuote(a)
	}
	return name + " " + strings.Join(q, " ")
}

// mergedJSON is existing overlaid with abby's owned keys.
func mergedJSON(existing, set jsonObject) jsonObject {
	out := append(jsonObject(nil), existing...)
	for _, f := range set {
		out = out.with(f.Key, f.Value)
	}
	return out
}

func (w *writer) cliAddChange(scope Scope, name string, e ServerEntry, existing jsonObject) (Change, error) {
	path, _ := w.ConfigPath(scope)
	after := mergedJSON(existing, ownedJSON(w.r, e))
	var cmds [][]string
	removeFirst := false // cmds[0] is a "remove" that must tolerate "already absent"
	switch w.r {
	case ClaudeCode:
		payload, _ := after.compact()
		if existing != nil {
			cmds = append(cmds, []string{"mcp", "remove", "-s", string(scope), name})
			removeFirst = true
		}
		cmds = append(cmds, []string{"mcp", "add-json", "-s", string(scope), name, string(payload)})
	case Gemini:
		args := []string{"mcp", "add", "-s", string(scope)}
		env := e.Env
		if env == nil {
			env = entryFromJSON(existing).Env
		}
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+env[k])
		}
		if e.Timeout >= time.Second {
			args = append(args, "--timeout", strconv.FormatInt(e.Timeout.Milliseconds(), 10))
		}
		args = append(args, name, e.Command)
		cmds = append(cmds, append(args, e.Args...))
	}
	var lines []string
	for _, c := range cmds {
		lines = append(lines, "$ "+commandLine(cliName(w.r), c))
	}
	c := Change{Runtime: w.r, Scope: scope, Method: MethodCLI, Target: path, Server: name,
		Noop:    existing != nil && renderJSONPreview(existing) == renderJSONPreview(after),
		Preview: lineDiff(renderJSONPreview(existing), renderJSONPreview(after)) + strings.Join(lines, "\n") + "\n"}
	c.apply = func() (string, error) {
		for i, args := range cmds {
			var err error
			if removeFirst && i == 0 {
				err = w.runRemove(scope, args...)
			} else {
				err = w.run(scope, args...)
			}
			if err != nil {
				if w.r == ClaudeCode && existing != nil && i > 0 {
					old, _ := existing.compact()
					if rerr := w.run(scope, "mcp", "add-json", "-s", string(scope), name, string(old)); rerr != nil {
						return "", fmt.Errorf("%w; restoring the previous entry also failed (%v) — re-add it with: %s",
							err, rerr, commandLine("claude", []string{"mcp", "add-json", "-s", string(scope), name, string(old)}))
					}
					return "", fmt.Errorf("%w; the previous entry was restored", err)
				}
				return "", err
			}
		}
		return "", nil
	}
	return c, nil
}

func (w *writer) cliRemoveChange(scope Scope, name string, existing jsonObject) (Change, error) {
	path, _ := w.ConfigPath(scope)
	c := Change{Runtime: w.r, Scope: scope, Method: MethodCLI, Target: path, Server: name, Remove: true, Noop: existing == nil}
	args := []string{"mcp", "remove", "-s", string(scope), name}
	c.Preview = lineDiff(renderJSONPreview(existing), "") + "$ " + commandLine(cliName(w.r), args) + "\n"
	c.apply = func() (string, error) { return "", w.runRemove(scope, args...) }
	return c, nil
}
