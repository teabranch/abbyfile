// Package sandbox confines abbyfile built-in tools: file paths must stay
// inside allowed directories, and run_command runs only allowlisted argv
// vectors without a shell. It imports nothing from this module so every
// other package can depend on it.
package sandbox

import (
	"fmt"
	"strings"
)

// shellOperators are rejected when they appear unquoted. There is no shell
// in restricted mode, so they would otherwise be passed to the program as
// literal arguments — rejecting them makes the model's mistake visible.
const shellOperators = ";&|`<>"

// SplitArgv splits a command line into argv using POSIX shell quoting rules:
// single quotes are literal, double quotes honor \ before $ ` " \, and a
// backslash outside quotes escapes the next character. Unquoted shell
// operators, "$(", and any newline are a parse error. Nothing is expanded.
func SplitArgv(s string) ([]string, error) {
	const (
		none = iota
		single
		double
	)
	var (
		argv   []string
		cur    strings.Builder
		inWord bool
		quote  = none
	)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch quote {
		case single:
			if c == '\'' {
				quote = none
			} else {
				cur.WriteRune(c)
			}
			continue
		case double:
			switch {
			case c == '"':
				quote = none
			case c == '\\' && i+1 < len(rs) && strings.ContainsRune("$`\"\\", rs[i+1]):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(c)
			}
			continue
		}
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				argv = append(argv, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\n':
			return nil, fmt.Errorf("newline is not allowed (commands cannot be chained)")
		case c == '\'':
			quote, inWord = single, true
		case c == '"':
			quote, inWord = double, true
		case c == '\\':
			if i+1 >= len(rs) {
				return nil, fmt.Errorf("trailing backslash")
			}
			if rs[i+1] == '\n' {
				return nil, fmt.Errorf("newline is not allowed (commands cannot be chained)")
			}
			i++
			cur.WriteRune(rs[i])
			inWord = true
		case c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			return nil, fmt.Errorf("command substitution $( is not allowed")
		case strings.ContainsRune(shellOperators, c):
			return nil, fmt.Errorf("shell operator %q is not allowed", string(c))
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if quote != none {
		return nil, fmt.Errorf("unterminated quote")
	}
	if inWord {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("command is empty")
	}
	return argv, nil
}
