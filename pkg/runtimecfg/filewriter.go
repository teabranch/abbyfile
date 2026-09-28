package runtimecfg

import (
	"errors"
	"fmt"

	"github.com/teabranch/abbyfile/pkg/fsutil"
)

// writer implements ConfigWriter for one runtime. Task 5 adds CLI methods;
// here every change uses the file method.
type writer struct {
	r    Runtime
	opts Options
}

func newWriter(r Runtime, opts Options) ConfigWriter { return &writer{r: r, opts: opts} }

func (w *writer) Runtime() Runtime                   { return w.r }
func (w *writer) ConfigPath(s Scope) (string, error) { return configPath(w.r, s, w.opts.ProjectRoot) }
func (w *writer) isTOML() bool                       { return w.r == Codex }

// notes are caveats shown for file-method changes.
func (w *writer) notes(scope Scope) []string {
	switch {
	case w.r == Codex && scope == ScopeProject:
		return []string{"Codex loads project .codex/config.toml only in trusted projects"}
	case w.r == ClaudeCode && scope == ScopeUser:
		return []string{"a running Claude Code rewrites ~/.claude.json and can overwrite this entry; install with `claude` on PATH or quit Claude Code first"}
	}
	return nil
}

// Lookup reads the entry from the config file (whatever method wrote it).
func (w *writer) Lookup(scope Scope, name string) (ServerEntry, bool, error) {
	path, err := w.ConfigPath(scope)
	if err != nil {
		return ServerEntry{}, false, err
	}
	snap, err := fsutil.ReadSnapshot(path)
	if err != nil || !snap.Exists {
		return ServerEntry{}, false, err
	}
	if w.isTOML() {
		m, ok, err := lookupTOMLServer(snap.Data, name)
		if err != nil {
			return ServerEntry{}, false, fmt.Errorf("%s: %w", path, err)
		}
		return entryFromTOML(m), ok, nil
	}
	o, ok, err := lookupJSONServer(snap.Data, name)
	if err != nil {
		return ServerEntry{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return entryFromJSON(o), ok, nil
}

// edit computes the new file content for data; before/after are rendered entries for previews.
type edit func(data []byte) (out []byte, before, after string, changed bool, err error)

func (w *writer) addEdit(name string, e ServerEntry) edit {
	if w.isTOML() {
		return func(data []byte) ([]byte, string, string, bool, error) {
			out, before, after, err := upsertTOMLServer(data, name, ownedTOML(e))
			if err != nil {
				return nil, "", "", false, err
			}
			beforeStr, afterStr := renderTOMLPreview(name, before), renderTOMLPreview(name, after)
			return out, beforeStr, afterStr, beforeStr != afterStr, nil
		}
	}
	return func(data []byte) ([]byte, string, string, bool, error) {
		out, before, after, err := upsertJSONServer(data, name, ownedJSON(w.r, e))
		if err != nil {
			return nil, "", "", false, err
		}
		beforeStr, afterStr := renderJSONPreview(before), renderJSONPreview(after)
		return out, beforeStr, afterStr, beforeStr != afterStr, nil
	}
}

func (w *writer) removeEdit(name string) edit {
	if w.isTOML() {
		return func(data []byte) ([]byte, string, string, bool, error) {
			out, before, found, err := removeTOMLServer(data, name)
			return out, renderTOMLPreview(name, before), "", found, err
		}
	}
	return func(data []byte) ([]byte, string, string, bool, error) {
		out, before, found, err := removeJSONServer(data, name)
		return out, renderJSONPreview(before), "", found, err
	}
}

// fileChange plans an edit of path and returns a Change whose Apply re-reads
// the file, re-applies the edit and commits; one concurrent modification is
// absorbed by re-planning, a second is an error.
func (w *writer) fileChange(scope Scope, name string, remove bool, ed edit) (Change, error) {
	path, err := w.ConfigPath(scope)
	if err != nil {
		return Change{}, err
	}
	snap, err := fsutil.ReadSnapshot(path)
	if err != nil {
		return Change{}, err
	}
	_, before, after, changed, err := ed(snap.Data)
	if err != nil {
		return Change{}, fmt.Errorf("%s: %w; abby did not modify it", path, err)
	}
	c := Change{Runtime: w.r, Scope: scope, Method: MethodFile, Target: path, Server: name, Remove: remove,
		Noop: !changed, Preview: lineDiff(before, after), Notes: w.notes(scope)}
	c.apply = func() (string, error) {
		for attempt := 0; ; attempt++ {
			s, err := fsutil.ReadSnapshot(path)
			if err != nil {
				return "", err
			}
			out, _, _, changed, err := ed(s.Data)
			if err != nil {
				return "", fmt.Errorf("%s: %w; abby did not modify it", path, err)
			}
			if !changed {
				return "", nil
			}
			backup, err := s.Commit(out)
			if errors.Is(err, fsutil.ErrChangedOnDisk) && attempt == 0 {
				continue
			}
			if errors.Is(err, fsutil.ErrChangedOnDisk) {
				return "", fmt.Errorf("%s kept changing while abby was writing it (is %s running?); nothing was written, retry: %w", path, w.r, err)
			}
			return backup, err
		}
	}
	return c, nil
}

func (w *writer) PlanAdd(scope Scope, name string, e ServerEntry) (Change, error) {
	existing, err := w.existingJSON(scope, name)
	if err != nil {
		return Change{}, err
	}
	m, err := w.choose(scope, &e, existing)
	if err != nil {
		return Change{}, err
	}
	if m == MethodCLI {
		return w.cliAddChange(scope, name, e, existing)
	}
	return w.fileChange(scope, name, false, w.addEdit(name, e))
}

func (w *writer) PlanRemove(scope Scope, name string) (Change, error) {
	existing, err := w.existingJSON(scope, name)
	if err != nil {
		return Change{}, err
	}
	m, err := w.choose(scope, nil, existing)
	if err != nil {
		return Change{}, err
	}
	if m == MethodCLI {
		return w.cliRemoveChange(scope, name, existing)
	}
	return w.fileChange(scope, name, true, w.removeEdit(name))
}

func renderJSONPreview(o jsonObject) string {
	if o == nil {
		return ""
	}
	b, err := o.marshal()
	if err != nil {
		return ""
	}
	return string(b)
}

func renderTOMLPreview(name string, m map[string]any) string {
	if m == nil {
		return ""
	}
	s, err := renderTOMLServer(name, m)
	if err != nil {
		return ""
	}
	return s
}
