package runtimecfg

import (
	"encoding/json"
	"strings"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestParseJSONObjectPreservesOrderAndNumbers(t *testing.T) {
	in := `{"z":1,"big":12345678901234567890,"a":{"k":[1,2]}}`
	o, err := parseJSONObject([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if o[0].Key != "z" || o[1].Key != "big" || o[2].Key != "a" {
		t.Fatalf("order lost: %+v", o)
	}
	out, _ := o.marshal()
	if !strings.Contains(string(out), "12345678901234567890") {
		t.Errorf("big number mangled: %s", out)
	}
	if strings.Index(string(out), `"z"`) > strings.Index(string(out), `"a"`) {
		t.Errorf("order not preserved on marshal: %s", out)
	}
}

func TestParseJSONObjectRejectsNonObjectAndComments(t *testing.T) {
	for _, in := range []string{`[1]`, `{"a":1 // c` + "\n}", `{"a":1,}`, `nope`} {
		if _, err := parseJSONObject([]byte(in)); err == nil {
			t.Errorf("parseJSONObject(%q) must fail", in)
		}
	}
	if o, err := parseJSONObject([]byte("  \n")); err != nil || len(o) != 0 {
		t.Errorf("empty input = %v, %v; want empty object", o, err)
	}
}

// Review Focus #1.
func TestUpsertJSONServer_PreservesUnownedKeys(t *testing.T) {
	in := `{"theme":"dark","mcpServers":{"other":{"command":"x"},"a":{"command":"/old","trust":true,"env":{"K":"v"},"includeTools":["t"]}},"tail":{"n":1}}`
	set := jsonObject{{"command", raw(`"/new"`)}, {"args", raw(`["serve-mcp"]`)}}
	out, before, after, err := upsertJSONServer([]byte(in), "a", set)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"theme": "dark"`, `"other"`, `"trust": true`, `"K": "v"`, `"includeTools"`, `"/new"`, `"tail"`} {
		if !strings.Contains(s, want) {
			t.Errorf("output lost %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"/old"`) {
		t.Errorf("command not replaced:\n%s", s)
	}
	if _, ok := before.get("trust"); !ok {
		t.Error("before must report the previous entry")
	}
	if v, _ := after.get("command"); string(v) != `"/new"` {
		t.Errorf("after.command = %s", v)
	}
}

func TestUpsertJSONServer_CreatesContainer(t *testing.T) {
	out, before, _, err := upsertJSONServer(nil, "a", jsonObject{{"command", raw(`"/bin/x"`)}})
	if err != nil || before != nil {
		t.Fatalf("err=%v before=%v", err, before)
	}
	if !strings.Contains(string(out), `"mcpServers"`) || !strings.HasSuffix(string(out), "}\n") {
		t.Errorf("out = %s", out)
	}
}

func TestUpsertJSONServer_RejectsNonObjectContainer(t *testing.T) {
	if _, _, _, err := upsertJSONServer([]byte(`{"mcpServers":[]}`), "a", jsonObject{}); err == nil {
		t.Fatal("mcpServers that is not an object must be refused")
	}
}

func TestRemoveAndLookupJSONServer(t *testing.T) {
	in := `{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"}},"k":1}`
	e, ok, err := lookupJSONServer([]byte(in), "a")
	if err != nil || !ok {
		t.Fatalf("lookup = %v %v", ok, err)
	}
	if v, _ := e.get("command"); string(v) != `"x"` {
		t.Errorf("lookup command = %s", v)
	}
	out, before, found, err := removeJSONServer([]byte(in), "a")
	if err != nil || !found || before == nil {
		t.Fatalf("remove = %v %v", found, err)
	}
	if strings.Contains(string(out), `"a"`) || !strings.Contains(string(out), `"b"`) || !strings.Contains(string(out), `"k": 1`) {
		t.Errorf("remove output = %s", out)
	}
	if _, _, found, _ := removeJSONServer([]byte(in), "missing"); found {
		t.Error("missing name must report found=false")
	}
}
