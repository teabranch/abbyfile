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
// A block starts at [mcp_servers.<name>] and ends after the last non-blank,
// non-comment line of that section (preserving trailing blank/comment lines).
// Includes [[mcp_servers.<name>.*]] array-of-tables (path length ≥3) but
// skips [[mcp_servers.<name>]] and [[mcp_servers.<name>]] (length 2).
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
		isArrayOfTables := strings.HasPrefix(strings.TrimSpace(lines[start]), "[[")
		path := splitTOMLKey(m[1])

		// Skip if not mcp_servers.<name>.*
		if len(path) < 2 || path[0] != codexServersKey || path[1] != name {
			continue
		}

		// For array-of-tables: skip [[mcp_servers.<name>]] (length 2); include [[mcp_servers.<name>.*]] (length ≥3)
		// For regular tables: include both [mcp_servers.<name>] (length 2) and subtables [mcp_servers.<name>.*] (length ≥3)
		if isArrayOfTables && len(path) < 3 {
			continue
		}

		// Find the end: last non-blank, non-comment line of this section
		end := len(lines)
		if hi+1 < len(headers) {
			end = headers[hi+1]
		}

		// Trim trailing blank/comment lines from this block
		blockEnd := start + 1
		for j := start + 1; j < end; j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				blockEnd = j + 1
			}
		}
		blocks = append(blocks, [2]int{start, blockEnd})
	}
	return blocks
}

func decodeServer(text, name string) (map[string]any, bool, error) {
	// The TOML decoder can't handle \r, so normalize to \n only
	normalizedText := strings.ReplaceAll(text, "\r\n", "\n")
	normalizedText = strings.ReplaceAll(normalizedText, "\r", "\n")
	var doc map[string]any
	if _, err := toml.Decode(normalizedText, &doc); err != nil {
		return nil, false, err
	}
	srv, _ := doc[codexServersKey].(map[string]any)
	e, ok := srv[name].(map[string]any)
	return e, ok, nil
}

// renderTOMLServer renders entry as a [mcp_servers.<name>] block (with any
// nested tables as dotted subtable headers), ending in a newline. name is the
// raw server name; the encoder quotes it when it isn't a bare key.
// lineSep is the line separator to use (e.g., "\n" or "\r\n").
func renderTOMLServer(name string, entry map[string]any, lineSep string) (string, error) {
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
	result := strings.TrimRight(s, "\n") + "\n"
	// Convert line endings if needed
	if lineSep == "\r\n" {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}
	return result, nil
}

// upsertTOMLServer merges set into [mcp_servers.<name>] and replaces only
// that block's lines (appending a new block when absent).
func upsertTOMLServer(data []byte, name string, set map[string]any) (out []byte, before, after map[string]any, err error) {
	text := string(data)
	existing, inDoc, err := decodeServer(text, name)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("not valid TOML: %w", err)
	}

	// Detect line ending (CRLF vs LF)
	var lineSep string
	if strings.Contains(text, "\r\n") {
		lineSep = "\r\n"
	} else {
		lineSep = "\n"
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
	block, err := renderTOMLServer(name, after, lineSep)
	if err != nil {
		return nil, nil, nil, err
	}
	var b strings.Builder
	if len(blocks) == 0 {
		b.WriteString(strings.TrimRight(text, "\r\n"))
		if b.Len() > 0 {
			b.WriteString(lineSep)
			b.WriteString(lineSep)
		}
		b.WriteString(block)
	} else {
		// Replace from the first block start to the last block end (includes all subtables)
		firstBlockStart := blocks[0][0]
		lastBlockEnd := blocks[len(blocks)-1][1]

		// Write lines before the first block
		b.WriteString(strings.Join(lines[0:firstBlockStart], "\n"))
		if firstBlockStart > 0 {
			b.WriteString("\n")
		}
		// Write the new block (with all nested tables, ends with \n)
		b.WriteString(block)
		// Write lines after the last block, preserving blank lines
		if lastBlockEnd < len(lines) {
			remaining := lines[lastBlockEnd:]
			// Count leading blank lines; keep only one if any exist
			blankCount := 0
			for _, l := range remaining {
				if strings.TrimSpace(l) == "" {
					blankCount++
				} else {
					break
				}
			}
			if blankCount > 0 {
				b.WriteString("\n")
				remaining = remaining[blankCount:]
			}
			if len(remaining) > 0 {
				b.WriteString(strings.Join(remaining, "\n"))
			}
		}
	}
	result := b.String()

	// Convert line endings if CRLF
	if lineSep == "\r\n" {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}

	out = []byte(result)

	// Stronger round-trip validation: decode and compare normalized forms
	decoded, ok, verr := decodeServer(string(out), name)
	if verr != nil || !ok {
		return nil, nil, nil, fmt.Errorf("internal error: edited TOML does not round-trip (%v)", verr)
	}

	// Compare normalized forms (re-encode both)
	expectedEncoded, _ := renderTOMLServer(name, after, "\n")
	actualEncoded, _ := renderTOMLServer(name, decoded, "\n")
	if expectedEncoded != actualEncoded {
		return nil, nil, nil, fmt.Errorf("internal error: entry mismatch after editing (expected %v, got %v)", after, decoded)
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

	// Detect line ending (CRLF vs LF)
	var lineSep string
	if strings.Contains(text, "\r\n") {
		lineSep = "\r\n"
	} else {
		lineSep = "\n"
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
	result := strings.Join(kept, "\n")

	// Convert line endings if CRLF
	if lineSep == "\r\n" {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}

	out = []byte(result)

	// Stronger round-trip validation: ensure entry is gone
	_, still, verr := decodeServer(string(out), name)
	if verr != nil || still {
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
