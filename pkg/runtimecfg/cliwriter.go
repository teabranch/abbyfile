package runtimecfg

import (
	"context"
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

func (w *writer) run(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	_, stderr, err := w.opts.Run(ctx, cliName(w.r), args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", cliName(w.r), strings.Join(args, " "), err, strings.TrimSpace(string(stderr)))
	}
	return nil
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
	switch w.r {
	case ClaudeCode:
		payload, _ := after.compact()
		if existing != nil {
			cmds = append(cmds, []string{"mcp", "remove", "-s", string(scope), name})
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
			if err := w.run(args...); err != nil {
				if w.r == ClaudeCode && existing != nil && i > 0 {
					old, _ := existing.compact()
					if rerr := w.run("mcp", "add-json", "-s", string(scope), name, string(old)); rerr != nil {
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
	c.apply = func() (string, error) { return "", w.run(args...) }
	return c, nil
}
