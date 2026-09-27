package builtins

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

// displayPath re-expresses hit (an absolute path under resolvedBase) relative
// to the caller's original base argument, so output keeps the shape the
// model asked for: "sub/a.go" for base ".", absolute for an absolute base.
func displayPath(base, resolvedBase, hit string) string {
	rel, err := filepath.Rel(resolvedBase, hit)
	if err != nil {
		return hit
	}
	return filepath.Join(base, rel)
}

// allowedEntry reports whether a walked entry may be used, and the path to
// operate on. Regular entries under a resolved root are inside by
// construction and are returned as-is. Symlinks are resolved and checked,
// because WalkDir reports them without following; the caller must then open
// the returned (resolved) path rather than the walked one, so that a
// symlink swapped after this check no longer affects what gets read.
func allowedEntry(sb *sandbox.Sandbox, path string, d fs.DirEntry) (string, bool) {
	if d.Type()&fs.ModeSymlink == 0 {
		return path, true
	}
	resolved, err := sb.Resolve(path, sandbox.Read)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// withSkipNote appends a note when entries outside the sandbox were skipped.
func withSkipNote(out string, skipped int) string {
	if skipped == 0 {
		return out
	}
	return fmt.Sprintf("%s\n(%d entries outside the allowed directories were skipped)", out, skipped)
}
