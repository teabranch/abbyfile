package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// resolveLenient returns p with every symlink evaluated. For paths that do
// not exist yet (write targets, lazily-created dirs) it evaluates the
// deepest existing ancestor and appends the rest. A dangling symlink
// anywhere on the path is an error: following it later could create a file
// outside the sandbox.
func resolveLenient(p string) (string, error) {
	cur := filepath.Clean(p)
	var rest []string // components below cur, outermost first
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symlink whose target does not exist", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// isRoot reports whether p is a filesystem root ("/").
func isRoot(p string) bool { return filepath.Dir(p) == p }

// within reports whether p is root or below it. Both must be clean,
// absolute and symlink-resolved. Comparison is per path element, so
// "/a-evil" is not within "/a".
func within(root, p string) bool {
	if isRoot(root) {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
