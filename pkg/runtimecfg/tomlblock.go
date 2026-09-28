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

// isBlank returns true if the line is blank (only whitespace).
func isBlank(l string) bool {
	return strings.TrimSpace(l) == ""
}

// isComment returns true if the line is a comment.
func isComment(l string) bool {
	return strings.HasPrefix(strings.TrimSpace(l), "#")
}

// segment is a run of lines: the preamble (before the first header) or one
// header line plus the lines up to the next header. Lines are verbatim (LF).
type segment struct {
	lines  []string
	target bool // header belongs to mcp_servers.<name> (table or [[…]] subtable, path ≥3 for [[]])
}

// splitSegments splits LF text into segments.
func splitSegments(lines []string, name string) []segment {
	var headers []int
	for i, l := range lines {
		if tomlHeader.MatchString(l) {
			headers = append(headers, i)
		}
	}

	var segments []segment

	// Preamble (before first header)
	if len(headers) > 0 {
		segments = append(segments, segment{
			lines:  lines[0:headers[0]],
			target: false,
		})
	} else {
		// No headers: entire file is preamble
		segments = append(segments, segment{
			lines:  lines,
			target: false,
		})
		return segments
	}

	// Each header + its content
	for i, start := range headers {
		end := len(lines)
		if i+1 < len(headers) {
			end = headers[i+1]
		}

		// Determine if this header is a target
		m := tomlHeader.FindStringSubmatch(lines[start])
		isArrayOfTables := strings.HasPrefix(strings.TrimSpace(lines[start]), "[[")
		path := splitTOMLKey(m[1])

		target := false
		if len(path) >= 2 && path[0] == codexServersKey && path[1] == name {
			if isArrayOfTables && len(path) >= 3 {
				target = true
			} else if !isArrayOfTables {
				target = true
			}
		}

		segments = append(segments, segment{
			lines:  lines[start:end],
			target: target,
		})
	}

	return segments
}

// detachTails detaches trailing blank/comment lines from target segments.
// Returns the modified segments with tails split into separate non-target segments.
func detachTails(segs []segment) []segment {
	var result []segment
	for _, seg := range segs {
		if !seg.target || len(seg.lines) == 0 {
			result = append(result, seg)
			continue
		}

		// Find the last non-blank, non-comment line
		lastContent := 0
		for i := 1; i < len(seg.lines); i++ {
			if !isBlank(seg.lines[i]) && !isComment(seg.lines[i]) {
				lastContent = i
			}
		}

		// Split: content + detached tail
		contentEnd := lastContent + 1
		if contentEnd < len(seg.lines) {
			result = append(result, segment{
				lines:  seg.lines[0:contentEnd],
				target: true,
			})
			result = append(result, segment{
				lines:  seg.lines[contentEnd:],
				target: false,
			})
		} else {
			result = append(result, seg)
		}
	}
	return result
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
	// Normalize line endings at the START: detect CRLF, convert to LF, work in LF
	crlf := bytes.Contains(data, []byte("\r\n"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	existing, inDoc, err := decodeServer(text, name)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("not valid TOML: %w", err)
	}

	lines := strings.Split(text, "\n")
	if inDoc {
		// Check for inline form
		var doc map[string]any
		if _, err := toml.Decode(text, &doc); err == nil {
			srv, _ := doc[codexServersKey].(map[string]any)
			if _, ok := srv[name]; ok {
				// Entry exists; verify it's not inline by checking segments
				segs := splitSegments(lines, name)
				hasTargetSeg := false
				for _, seg := range segs {
					if seg.target {
						hasTargetSeg = true
						break
					}
				}
				if !hasTargetSeg {
					return nil, nil, nil, fmt.Errorf("mcp_servers.%s is defined inline; abby only edits [mcp_servers.%s] tables — edit it by hand or remove it and re-run", name, name)
				}
			}
		}
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

	// Segment-based assembly
	segs := splitSegments(lines, name)
	segs = detachTails(segs)

	var pieces []string
	inserted := false
	for _, seg := range segs {
		if seg.target {
			if !inserted {
				// Trim the rendered block for assembly
				trimmed := strings.TrimRight(block, "\n")
				if trimmed != "" {
					pieces = append(pieces, trimmed)
				}
				inserted = true
			}
			continue
		}

		// Non-target segment: trim leading/trailing blanks, keep content
		segText := strings.Join(seg.lines, "\n")
		// Trim leading blanks
		segText = strings.TrimLeft(segText, "\n")
		// Trim trailing blanks
		segText = strings.TrimRight(segText, "\n")
		if segText != "" {
			pieces = append(pieces, segText)
		}
	}

	// Append case: if we haven't inserted yet, add the rendered block
	if !inserted && inDoc {
		trimmed := strings.TrimRight(block, "\n")
		if trimmed != "" {
			pieces = append(pieces, trimmed)
		}
	} else if !inserted && !inDoc {
		// Append new entry to file
		trimmed := strings.TrimRight(block, "\n")
		if trimmed != "" {
			pieces = append(pieces, trimmed)
		}
	}

	// Assemble with one blank line between pieces
	var result string
	if len(pieces) == 0 {
		result = ""
	} else {
		result = strings.Join(pieces, "\n\n") + "\n"
	}

	// Validate round-trip before line-ending conversion
	decoded, ok, verr := decodeServer(result, name)
	if verr != nil || !ok {
		return nil, nil, nil, fmt.Errorf("internal error: edited TOML does not round-trip (%v)", verr)
	}

	// Compare normalized forms
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

	// Check for inline form
	segs := splitSegments(lines, name)
	hasTargetSeg := false
	for _, seg := range segs {
		if seg.target {
			hasTargetSeg = true
			break
		}
	}
	if !hasTargetSeg {
		return nil, nil, false, fmt.Errorf("mcp_servers.%s is defined inline; remove it by hand", name)
	}

	// Segment-based assembly: skip all target segments
	segs = detachTails(segs)

	var pieces []string
	for _, seg := range segs {
		if seg.target {
			continue
		}

		// Non-target segment: trim leading/trailing blanks
		segText := strings.Join(seg.lines, "\n")
		// Trim leading blanks
		segText = strings.TrimLeft(segText, "\n")
		// Trim trailing blanks
		segText = strings.TrimRight(segText, "\n")
		if segText != "" {
			pieces = append(pieces, segText)
		}
	}

	// Assemble with one blank line between pieces
	var result string
	if len(pieces) == 0 {
		result = ""
	} else {
		result = strings.Join(pieces, "\n\n") + "\n"
	}

	// Validate round-trip before line-ending conversion
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
