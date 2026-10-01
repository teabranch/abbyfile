package runtimecfg

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// redactedChanged replaces an env value in the "after" side of an entry diff
// when it differs from the "before" side, so a change to a secret shows as a
// change without revealing either value.
const redactedChanged = "*** (changed)"

// jsonPreviewPair renders before/after JSON entries for an entry diff, with
// env values redacted. The unredacted renderings decide whether anything
// changed; these are for display only.
func jsonPreviewPair(before, after jsonObject) (string, string) {
	prev := envJSONValues(before)
	b := before
	if b != nil {
		b = maskEnvJSON(b, func(string, json.RawMessage) string { return redacted })
	}
	a := after
	if a != nil {
		a = maskEnvJSON(a, func(k string, v json.RawMessage) string {
			if old, ok := prev[k]; ok && !bytes.Equal(old, v) {
				return redactedChanged
			}
			return redacted
		})
	}
	return renderJSONPreview(b), renderJSONPreview(a)
}

func envJSONValues(entry jsonObject) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	v, ok := entry.get("env")
	if !ok {
		return out
	}
	env, err := parseJSONObject(v)
	if err != nil {
		return out
	}
	for _, f := range env {
		out[f.Key] = f.Value
	}
	return out
}

// maskEnvJSON returns entry with each env value replaced by mask(key, value).
// entry itself is not modified.
func maskEnvJSON(entry jsonObject, mask func(string, json.RawMessage) string) jsonObject {
	v, ok := entry.get("env")
	if !ok {
		return entry
	}
	env, err := parseJSONObject(v)
	if err != nil {
		return entry
	}
	masked := make(jsonObject, len(env))
	for i, f := range env {
		s, _ := json.Marshal(mask(f.Key, f.Value))
		masked[i] = jsonField{Key: f.Key, Value: s}
	}
	b, err := masked.compact()
	if err != nil {
		return entry
	}
	return entry.with("env", b)
}

// tomlPreviewPair is jsonPreviewPair for Codex entries.
func tomlPreviewPair(name string, before, after map[string]any) (string, string) {
	prev, _ := before["env"].(map[string]any)
	b := maskEnvTOML(before, func(string, any) string { return redacted })
	a := maskEnvTOML(after, func(k string, v any) string {
		if old, ok := prev[k]; ok && !reflect.DeepEqual(old, v) {
			return redactedChanged
		}
		return redacted
	})
	return renderTOMLPreview(name, b), renderTOMLPreview(name, a)
}

// maskEnvTOML returns a shallow copy of entry whose env table has each value
// replaced by mask(key, value). entry itself is not modified.
func maskEnvTOML(entry map[string]any, mask func(string, any) string) map[string]any {
	env, ok := entry["env"].(map[string]any)
	if !ok {
		return entry
	}
	masked := make(map[string]any, len(env))
	for k, v := range env {
		masked[k] = mask(k, v)
	}
	out := make(map[string]any, len(entry))
	for k, v := range entry {
		out[k] = v
	}
	out["env"] = masked
	return out
}
