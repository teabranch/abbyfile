package sandbox

import "fmt"

// AllowRule is one parsed sandbox.allow_commands entry. Prefix must match
// argv word-for-word; AnyArgs (a trailing "*") permits zero or more further
// arguments. There is no shell, so "*" never means "any shell text".
type AllowRule struct {
	Prefix  []string
	AnyArgs bool
}

// ParseAllowEntry parses an allow_commands entry with the same quoting rules
// as SplitArgv. "*" is only valid as the last word, and a bare "*" is
// rejected: allowing every program is what sandbox.bash: unrestricted is for.
func ParseAllowEntry(entry string) (AllowRule, error) {
	argv, err := SplitArgv(entry)
	if err != nil {
		return AllowRule{}, fmt.Errorf("allow_commands entry %q: %w", entry, err)
	}
	r := AllowRule{Prefix: argv}
	if argv[len(argv)-1] == "*" {
		r.AnyArgs = true
		r.Prefix = argv[:len(argv)-1]
	}
	if len(r.Prefix) == 0 {
		return AllowRule{}, fmt.Errorf("allow_commands entry %q: a bare * would allow every program; name the program, or use sandbox.bash: unrestricted", entry)
	}
	for _, w := range r.Prefix {
		if w == "*" {
			return AllowRule{}, fmt.Errorf("allow_commands entry %q: * is only allowed as the last word", entry)
		}
	}
	return r, nil
}

// Matches reports whether argv is permitted by the rule.
func (r AllowRule) Matches(argv []string) bool {
	if len(argv) < len(r.Prefix) {
		return false
	}
	if !r.AnyArgs && len(argv) != len(r.Prefix) {
		return false
	}
	for i, w := range r.Prefix {
		if argv[i] != w {
			return false
		}
	}
	return true
}
