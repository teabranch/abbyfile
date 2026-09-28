package runtimecfg

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const codexServersKey = "mcp_servers"

var tomlHeader = regexp.MustCompile(`^\s*\[\[?\s*([^\[\]]+?)\s*\]\]?\s*(#.*)?$`)

// splitTOMLKey splits a dotted TOML key, honouring "basic" and 'literal' quotes.
func splitTOMLKey(s string) []string {
	var parts []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		case c == ' ' || c == '\t':
		default:
			cur.WriteByte(c)
		}
	}
	return append(parts, strings.TrimSpace(cur.String()))
}

// serverBlocks returns [start,end) line ranges of the [mcp_servers.<name>]
// table and its subtables, plus the index of every header line.
func serverBlocks(lines []string, name string) [][2]int {
	var headers []int
	for i, l := range lines {
		if tomlHeader.MatchString(l) {
			headers = append(headers, i)
		}
	}
	var blocks [][2]int
	for hi, start := range headers {
		m := tomlHeader.FindStringSubmatch(lines[start])
		if strings.HasPrefix(strings.TrimSpace(lines[start]), "[[") {
			continue
		}
		path := splitTOMLKey(m[1])
		if len(path) < 2 || path[0] != codexServersKey || path[1] != name {
			continue
		}
		end := len(lines)
		if hi+1 < len(headers) {
			end = headers[hi+1]
		}
		blocks = append(blocks, [2]int{start, end})
	}
	return blocks
}

func decodeServer(text, name string) (map[string]any, bool, error) {
	var doc map[string]any
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, false, err
	}
	srv, _ := doc[codexServersKey].(map[string]any)
	e, ok := srv[name].(map[string]any)
	return e, ok, nil
}

// renderTOMLServer renders entry as a [mcp_servers.<name>] block (with any
// nested tables as dotted subtable headers), ending in a newline. name is the
// raw server name; the encoder quotes it when it isn't a bare key.
func renderTOMLServer(name string, entry map[string]any) (string, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(map[string]any{codexServersKey: map[string]any{name: entry}}); err != nil {
		return "", err
	}
	// Drop the encoder's bare "[mcp_servers]" header: defining that table
	// again would be invalid if the file already has one.
	var out []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(l) == "["+codexServersKey+"]" {
			continue
		}
		out = append(out, l)
	}
	s := strings.TrimLeft(strings.Join(out, "\n"), "\n")
	return strings.TrimRight(s, "\n") + "\n", nil
}

// upsertTOMLServer merges set into [mcp_servers.<name>] and replaces only
// that block's lines (appending a new block when absent).
func upsertTOMLServer(data []byte, name string, set map[string]any) (out []byte, before, after map[string]any, err error) {
	text := string(data)
	existing, inDoc, err := decodeServer(text, name)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("not valid TOML: %w", err)
	}
	lines := strings.Split(text, "\n")
	blocks := serverBlocks(lines, name)
	if inDoc && len(blocks) == 0 {
		return nil, nil, nil, fmt.Errorf("mcp_servers.%s is defined inline; abby only edits [mcp_servers.%s] tables — edit it by hand or remove it and re-run", name, name)
	}
	after = map[string]any{}
	if inDoc {
		before = existing
		for k, v := range existing {
			after[k] = v
		}
	}
	for k, v := range set {
		after[k] = v
	}
	block, err := renderTOMLServer(name, after)
	if err != nil {
		return nil, nil, nil, err
	}
	var b strings.Builder
	if len(blocks) == 0 {
		b.WriteString(strings.TrimRight(text, "\n"))
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(block)
	} else {
		pos := 0
		for i, r := range blocks {
			b.WriteString(strings.Join(lines[pos:r[0]], "\n"))
			if r[0] > pos {
				b.WriteString("\n")
			}
			if i == 0 {
				b.WriteString(block)
				if r[1] < len(lines) {
					b.WriteString("\n")
				}
			}
			pos = r[1]
		}
		b.WriteString(strings.Join(lines[pos:], "\n"))
	}
	out = []byte(b.String())
	if _, ok, verr := decodeServer(string(out), name); verr != nil || !ok {
		return nil, nil, nil, fmt.Errorf("internal error: edited TOML does not round-trip (%v)", verr)
	}
	return out, before, after, nil
}

// removeTOMLServer deletes the [mcp_servers.<name>] block(s).
func removeTOMLServer(data []byte, name string) (out []byte, before map[string]any, found bool, err error) {
	text := string(data)
	existing, inDoc, err := decodeServer(text, name)
	if err != nil {
		return nil, nil, false, fmt.Errorf("not valid TOML: %w", err)
	}
	if !inDoc {
		return data, nil, false, nil
	}
	lines := strings.Split(text, "\n")
	blocks := serverBlocks(lines, name)
	if len(blocks) == 0 {
		return nil, nil, false, fmt.Errorf("mcp_servers.%s is defined inline; remove it by hand", name)
	}
	var kept []string
	pos := 0
	for _, r := range blocks {
		kept = append(kept, lines[pos:r[0]]...)
		pos = r[1]
	}
	kept = append(kept, lines[pos:]...)
	out = []byte(strings.Join(kept, "\n"))
	if _, still, verr := decodeServer(string(out), name); verr != nil || still {
		return nil, nil, false, fmt.Errorf("internal error: removal did not round-trip (%v)", verr)
	}
	return out, existing, true, nil
}

// lookupTOMLServer returns [mcp_servers.<name>].
func lookupTOMLServer(data []byte, name string) (map[string]any, bool, error) {
	e, ok, err := decodeServer(string(data), name)
	if err != nil {
		return nil, false, fmt.Errorf("not valid TOML: %w", err)
	}
	return e, ok, nil
}
