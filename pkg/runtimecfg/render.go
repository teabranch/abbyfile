package runtimecfg

import (
	"encoding/json"
	"math"
	"sort"
	"time"
)

// CodexStartupTimeoutSec is written as startup_timeout_sec for Codex entries.
const CodexStartupTimeoutSec = 30

// ownedJSON renders the keys abby owns for a JSON runtime entry.
func ownedJSON(r Runtime, e ServerEntry) jsonObject {
	m := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	var o jsonObject
	if r == ClaudeCode {
		o = append(o, jsonField{"type", m("stdio")})
	}
	args := e.Args
	if args == nil {
		args = []string{}
	}
	o = append(o, jsonField{"command", m(e.Command)}, jsonField{"args", m(args)})
	if len(e.Env) > 0 {
		o = append(o, jsonField{"env", m(e.Env)})
	}
	if r == Gemini && e.Cwd != "" {
		o = append(o, jsonField{"cwd", m(e.Cwd)})
	}
	if e.Timeout >= time.Second {
		o = append(o, jsonField{"timeout", m(e.Timeout.Milliseconds())})
	}
	return o
}

// ownedTOML renders the keys abby owns for a Codex entry.
func ownedTOML(e ServerEntry) map[string]any {
	args := e.Args
	if args == nil {
		args = []string{}
	}
	m := map[string]any{"command": e.Command, "args": args, "startup_timeout_sec": int64(CodexStartupTimeoutSec)}
	if len(e.Env) > 0 {
		env := map[string]any{}
		for k, v := range e.Env {
			env[k] = v
		}
		m["env"] = env
	}
	if e.Cwd != "" {
		m["cwd"] = e.Cwd
	}
	if e.Timeout > 0 {
		m["tool_timeout_sec"] = int64(math.Ceil(e.Timeout.Seconds()))
	}
	return m
}

// ownedKeys lists the keys ownedJSON/ownedTOML may set, per runtime; used
// to decide whether an existing entry has keys a CLI would drop.
func ownedKeys(r Runtime) map[string]bool {
	switch r {
	case ClaudeCode:
		return map[string]bool{"type": true, "command": true, "args": true, "env": true, "timeout": true}
	case Gemini:
		return map[string]bool{"command": true, "args": true, "env": true, "cwd": true, "timeout": true}
	default:
		return map[string]bool{"command": true, "args": true, "env": true, "cwd": true, "startup_timeout_sec": true, "tool_timeout_sec": true}
	}
}

// entryFromJSON reads a ServerEntry back from a JSON entry.
func entryFromJSON(o jsonObject) ServerEntry {
	var e ServerEntry
	if v, ok := o.get("command"); ok {
		json.Unmarshal(v, &e.Command)
	}
	if v, ok := o.get("args"); ok {
		json.Unmarshal(v, &e.Args)
	}
	if v, ok := o.get("env"); ok {
		json.Unmarshal(v, &e.Env)
	}
	if v, ok := o.get("cwd"); ok {
		json.Unmarshal(v, &e.Cwd)
	}
	if v, ok := o.get("timeout"); ok {
		var ms int64
		if json.Unmarshal(v, &ms) == nil {
			e.Timeout = time.Duration(ms) * time.Millisecond
		}
	}
	return e
}

// entryFromTOML reads a ServerEntry back from a Codex entry.
func entryFromTOML(m map[string]any) ServerEntry {
	var e ServerEntry
	e.Command, _ = m["command"].(string)
	if as, ok := m["args"].([]any); ok {
		for _, a := range as {
			if s, ok := a.(string); ok {
				e.Args = append(e.Args, s)
			}
		}
	}
	if env, ok := m["env"].(map[string]any); ok {
		e.Env = map[string]string{}
		for k, v := range env {
			if s, ok := v.(string); ok {
				e.Env[k] = s
			}
		}
	}
	e.Cwd, _ = m["cwd"].(string)
	if s, ok := m["tool_timeout_sec"].(int64); ok {
		e.Timeout = time.Duration(s) * time.Second
	}
	return e
}

func sortedKeys(m map[string]bool) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
