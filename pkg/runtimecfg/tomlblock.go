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
// skips [[mcp_servers.<name>]] (length 2).
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

		// Trim trailing blank/comment lines from this block (don't include next header as content)
		blockEnd := start + 1
		for j := start + 1; j < end; j++ {
			trimmed := strings.TrimSpace(lines[j])
			// Stop if we hit another header (even if it's a subtable of ours)
			if tomlHeader.MatchString(lines[j]) {
				break
			}
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				blockEnd = j + 1
			}
		}
		blocks = append(blocks, [2]int{start, blockEnd})
	}
	return blocks
}

func decodeServer(text, name string) (map[string]any, bool, error) {
	// text is already normalized to LF only; decode directly
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
	// Normalize line endings at the START: detect CRLF, convert to LF, work in LF
	crlf := bytes.Contains(data, []byte("\r\n"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

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
		// Append: trim trailing newlines, add separator, add block
		b.WriteString(strings.TrimRight(text, "\n"))
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(block)
	} else {
		// Replace blocks using walk pattern: emit lines before first block,
		// rendered block at first position, gaps between blocks (preserving unrelated content),
		// skip subsequent blocks, emit lines after last block

		firstBlockStart := blocks[0][0]
		// Absorb leading blank lines before the first block
		absorbBlanksFrom := firstBlockStart
		for i := firstBlockStart - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) == "" {
				absorbBlanksFrom = i
			} else {
				break
			}
		}

		// Write lines before the first block (absorb leading blanks)
		if absorbBlanksFrom > 0 {
			b.WriteString(strings.Join(lines[0:absorbBlanksFrom], "\n"))
			b.WriteString("\n")
		}

		// Write the rendered block at the first block position
		b.WriteString(block)

		// Walk subsequent blocks: emit gaps, skip blocks
		for i := 1; i < len(blocks); i++ {
			prevBlockEnd := blocks[i-1][1]
			currBlockStart := blocks[i][0]

			// Emit gap between previous block end and current block start
			// Absorb leading blank lines before this block (only blanks)
			gap := lines[prevBlockEnd:currBlockStart]
			blanksBefore := 0
			for _, l := range gap {
				if strings.TrimSpace(l) == "" {
					blanksBefore++
				} else {
					break
				}
			}

			// Keep non-blank gap content, skip blanks
			if len(gap) > blanksBefore {
				// There's non-blank content in the gap; emit it with one blank line separator
				b.WriteString("\n")
				b.WriteString(strings.Join(gap[blanksBefore:], "\n"))
			}

			// Skip the current block (don't emit lines[currBlockStart:blocks[i][1]])
		}

		// Write lines after the last block
		lastBlockEnd := blocks[len(blocks)-1][1]
		if lastBlockEnd < len(lines) {
			remaining := lines[lastBlockEnd:]
			// Absorb leading blank lines after the last block (keep only one as seam separator)
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
	// Ensure result ends with exactly one newline (trim trailing blanks, add one newline)
	result = strings.TrimRight(result, "\n") + "\n"

	// Validate round-trip before any line-ending conversion
	decoded, ok, verr := decodeServer(result, name)
	if verr != nil || !ok {
		return nil, nil, nil, fmt.Errorf("internal error: edited TOML does not round-trip (%v)", verr)
	}

	// Compare normalized forms (re-encode both)
	expectedEncoded, _ := renderTOMLServer(name, after)
	actualEncoded, _ := renderTOMLServer(name, decoded)
	if expectedEncoded != actualEncoded {
		return nil, nil, nil, fmt.Errorf("internal error: entry mismatch after editing (expected %v, got %v)", after, decoded)
	}

	// Convert line endings at the END, exactly once, only if input had CRLF
	if crlf {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}

	out = []byte(result)
	return out, before, after, nil
}

// removeTOMLServer deletes the [mcp_servers.<name>] block(s).
func removeTOMLServer(data []byte, name string) (out []byte, before map[string]any, found bool, err error) {
	// Normalize line endings at the START: detect CRLF, convert to LF, work in LF
	crlf := bytes.Contains(data, []byte("\r\n"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

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

	// Walk blocks: keep lines before first block, skip blocks and gaps between them,
	// absorb blank lines before content after last block, keep remaining lines
	var kept []string
	firstBlockStart := blocks[0][0]

	// Absorb leading blank lines before the first block
	absorbBlanksFrom := firstBlockStart
	for i := firstBlockStart - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			absorbBlanksFrom = i
		} else {
			break
		}
	}

	// Keep lines before the first block (absorb leading blanks)
	if absorbBlanksFrom > 0 {
		kept = append(kept, lines[0:absorbBlanksFrom]...)
	}

	// Walk through blocks and their gaps: skip each block but keep gaps that contain unrelated content
	for i := 0; i < len(blocks); i++ {
		currBlockEnd := blocks[i][1]
		nextBlockStart := len(lines)
		if i+1 < len(blocks) {
			nextBlockStart = blocks[i+1][0]
		}

		// Gap between this block and the next (or end of file)
		gap := lines[currBlockEnd:nextBlockStart]
		// Preserve the gap as-is; it contains comments, blank lines, and other tables
		kept = append(kept, gap...)
	}

	// Trim trailing blank lines and ensure single newline at end
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}

	result := strings.Join(kept, "\n")
	// Ensure result ends with exactly one newline (trim trailing blanks, add one newline)
	result = strings.TrimRight(result, "\n")
	if result != "" {
		result += "\n"
	}

	// Validate round-trip before any line-ending conversion
	_, still, verr := decodeServer(result, name)
	if verr != nil || still {
		return nil, nil, false, fmt.Errorf("internal error: removal did not round-trip (%v)", verr)
	}

	// Convert line endings at the END, exactly once, only if input had CRLF
	if crlf {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}

	out = []byte(result)
	return out, existing, true, nil
}

// lookupTOMLServer returns [mcp_servers.<name>].
func lookupTOMLServer(data []byte, name string) (map[string]any, bool, error) {
	// Normalize line endings at the START: detect CRLF, convert to LF, work in LF
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	e, ok, err := decodeServer(text, name)
	if err != nil {
		return nil, false, fmt.Errorf("not valid TOML: %w", err)
	}
	return e, ok, nil
}
