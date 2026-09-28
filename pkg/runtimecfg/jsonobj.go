package runtimecfg

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// mcpServersKey is the container key Claude Code and Gemini CLI use.
const mcpServersKey = "mcpServers"

type jsonField struct {
	Key   string
	Value json.RawMessage
}

// jsonObject is a JSON object as an ordered list of raw values, so editing
// one key leaves every other key's order and bytes untouched (numbers are
// never round-tripped through float64).
type jsonObject []jsonField

// parseJSONObject parses a JSON object strictly (no comments, no trailing
// commas). Empty input is an empty object.
func parseJSONObject(data []byte) (jsonObject, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return jsonObject{}, nil
	}
	if !json.Valid(data) {
		// Always get the positioned error from json.Unmarshal
		var tmp any
		if err := json.Unmarshal(data, &tmp); err != nil {
			return nil, fmt.Errorf("not valid JSON: %w (note: comments and trailing commas are not supported)", err)
		}
		return nil, fmt.Errorf("not valid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("top level is not a JSON object")
	}
	o := jsonObject{} // Initialize as non-nil empty slice, not nil
	seen := make(map[string]bool)
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := kt.(string)
		if seen[key] {
			return nil, fmt.Errorf("duplicate key %q (abby refuses to edit files with duplicate keys; remove one)", key)
		}
		seen[key] = true
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, jsonField{Key: key, Value: v})
	}
	return o, nil
}

func (o jsonObject) get(key string) (json.RawMessage, bool) {
	for _, f := range o {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// with returns a copy of o with key set to v (replaced in place, else appended).
func (o jsonObject) with(key string, v json.RawMessage) jsonObject {
	out := make(jsonObject, 0, len(o)+1)
	replaced := false
	for _, f := range o {
		if f.Key == key {
			out = append(out, jsonField{Key: key, Value: v})
			replaced = true
			continue
		}
		out = append(out, f)
	}
	if !replaced {
		out = append(out, jsonField{Key: key, Value: v})
	}
	return out
}

// without returns a copy of o without key.
func (o jsonObject) without(key string) jsonObject {
	out := make(jsonObject, 0, len(o))
	for _, f := range o {
		if f.Key != key {
			out = append(out, f)
		}
	}
	return out
}

func (o jsonObject) compact() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		// Use Encoder with SetEscapeHTML(false) to preserve key characters like <, >, &
		var keyBuf bytes.Buffer
		enc := json.NewEncoder(&keyBuf)
		enc.SetEscapeHTML(false)
		enc.Encode(f.Key)
		// Remove the trailing newline added by Encode
		keyBytes := bytes.TrimSuffix(keyBuf.Bytes(), []byte("\n"))
		b.Write(keyBytes)
		b.WriteByte(':')
		b.Write(f.Value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshal renders o with 2-space indentation and a trailing newline.
func (o jsonObject) marshal() ([]byte, error) {
	c, err := o.compact()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, c, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// servers returns the mcpServers object of top (empty if absent or null).
func servers(top jsonObject) (jsonObject, error) {
	v, ok := top.get(mcpServersKey)
	if !ok {
		return jsonObject{}, nil
	}
	// A null container means "no servers" (some tools and hand edits write
	// "mcpServers": null); treat it like an absent key rather than refusing
	// the file. An upsert replaces it with an object.
	if string(bytes.TrimSpace(v)) == "null" {
		return jsonObject{}, nil
	}
	s, err := parseJSONObject(v)
	if err != nil {
		return nil, fmt.Errorf("%q is not a JSON object: %w", mcpServersKey, err)
	}
	return s, nil
}

// upsertJSONServer sets the fields in set on mcpServers[name], keeping all
// other keys of that entry, of mcpServers and of the document. before is
// the previous entry (nil when new); after is the new entry.
func upsertJSONServer(data []byte, name string, set jsonObject) (out []byte, before, after jsonObject, err error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, nil, nil, err
	}
	srv, err := servers(top)
	if err != nil {
		return nil, nil, nil, err
	}
	entry := jsonObject{}
	if v, ok := srv.get(name); ok {
		if entry, err = parseJSONObject(v); err != nil {
			return nil, nil, nil, fmt.Errorf("%s.%s is not a JSON object: %w", mcpServersKey, name, err)
		}
		before = entry
	}
	after = entry
	for _, f := range set {
		after = after.with(f.Key, f.Value)
	}
	ec, _ := after.compact()
	srv = srv.with(name, ec)
	sc, _ := srv.compact()
	top = top.with(mcpServersKey, sc)
	out, err = top.marshal()
	return out, before, after, err
}

// removeJSONServer deletes mcpServers[name]; found=false leaves data unchanged.
func removeJSONServer(data []byte, name string) (out []byte, before jsonObject, found bool, err error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, nil, false, err
	}
	srv, err := servers(top)
	if err != nil {
		return nil, nil, false, err
	}
	v, ok := srv.get(name)
	if !ok {
		return data, nil, false, nil
	}
	before, _ = parseJSONObject(v)
	sc, _ := srv.without(name).compact()
	out, err = top.with(mcpServersKey, sc).marshal()
	return out, before, true, err
}

// lookupJSONServer returns mcpServers[name].
func lookupJSONServer(data []byte, name string) (jsonObject, bool, error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, false, err
	}
	srv, err := servers(top)
	if err != nil {
		return nil, false, err
	}
	v, ok := srv.get(name)
	if !ok {
		return nil, false, nil
	}
	e, err := parseJSONObject(v)
	return e, err == nil, err
}
