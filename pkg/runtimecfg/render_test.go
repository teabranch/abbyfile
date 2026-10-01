package runtimecfg

import (
	"encoding/json"
	"testing"
	"time"
)

// Review Focus #3: entryFromJSON used to read "timeout" only as an integer
// (json.Unmarshal into int64 rejects a literal with a decimal point, even
// 1500.0), so a hand-edited float timeout silently read back as 0.
func TestEntryFromJSONAcceptsFloatTimeout(t *testing.T) {
	o := jsonObject{
		{Key: "command", Value: json.RawMessage(`"c"`)},
		{Key: "timeout", Value: json.RawMessage(`1500.0`)},
	}
	e := entryFromJSON(o)
	if e.Timeout != 1500*time.Millisecond {
		t.Errorf("Timeout = %v, want 1500ms", e.Timeout)
	}
}

// A fractional millisecond count must ceil, not truncate or silently fail.
func TestEntryFromJSONCeilsFractionalFloatTimeout(t *testing.T) {
	o := jsonObject{
		{Key: "command", Value: json.RawMessage(`"c"`)},
		{Key: "timeout", Value: json.RawMessage(`1500.4`)},
	}
	e := entryFromJSON(o)
	if e.Timeout != 1501*time.Millisecond {
		t.Errorf("Timeout = %v, want 1501ms (ceil of 1500.4)", e.Timeout)
	}
}

// entryFromTOML used to read tool_timeout_sec only as int64; the TOML
// decoder gives float64 for a literal with a decimal point (e.g. a
// hand-edited 120.5), which used to silently read back as 0.
func TestEntryFromTOMLAcceptsFloatTimeout(t *testing.T) {
	m := map[string]any{"command": "c", "tool_timeout_sec": 120.5}
	e := entryFromTOML(m)
	if e.Timeout != 121*time.Second {
		t.Errorf("Timeout = %v, want 121s (ceil of 120.5)", e.Timeout)
	}
}

// The normal integer case (as the real TOML decoder produces for a plain
// integer literal) must still work.
func TestEntryFromTOMLAcceptsIntTimeout(t *testing.T) {
	m := map[string]any{"command": "c", "tool_timeout_sec": int64(126)}
	e := entryFromTOML(m)
	if e.Timeout != 126*time.Second {
		t.Errorf("Timeout = %v, want 126s", e.Timeout)
	}
}
