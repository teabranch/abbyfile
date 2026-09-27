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

// allowedEntry reports whether a walked entry may be used. Regular entries
// under a resolved root are inside by construction; symlinks are resolved
// and checked because WalkDir reports them without following.
func allowedEntry(sb *sandbox.Sandbox, path string, d fs.DirEntry) bool {
	if d.Type()&fs.ModeSymlink == 0 {
		return true
	}
	_, err := sb.Resolve(path, sandbox.Read)
	return err == nil
}

// withSkipNote appends a note when entries outside the sandbox were skipped.
func withSkipNote(out string, skipped int) string {
	if skipped == 0 {
		return out
	}
	return fmt.Sprintf("%s\n(%d entries outside the allowed directories were skipped)", out, skipped)
}
