//go:build unix

package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// geteuid, chownFile and chownPath are replaced in tests.
var (
	geteuid   = os.Geteuid
	chownFile = func(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }
	chownPath = os.Lchown
)

// ownerToRestore decides whether a rewrite of the file described by fi, by a
// process with effective uid euid, must be chowned back to the file's owner.
// That is only the case under root (e.g. sudo abby install --global) when the
// file belongs to someone else: the atomic rename would otherwise leave the
// user's config owned by root.
func ownerToRestore(fi fs.FileInfo, euid int) (owner, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil || euid != 0 || int(st.Uid) == euid {
		return owner{}, false
	}
	return owner{uid: int(st.Uid), gid: int(st.Gid)}, true
}

// restoreOwnerFunc returns a hook that chowns a temp file to the owner of the
// existing file at path, or nil when no chown is needed.
func restoreOwnerFunc(path string) func(*os.File) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	o, need := ownerToRestore(fi, geteuid())
	if !need {
		return nil
	}
	return func(f *os.File) error { return chownFile(f, o.uid, o.gid) }
}

// inheritOwnerFunc is restoreOwnerFunc for a file that doesn't exist yet:
// under root, the directories in created (made by Commit, outermost first)
// are chowned now to the owner of ancestor, the nearest directory that
// already existed, and the returned hook chowns the new file the same way.
// It returns nil when no chown is needed.
func inheritOwnerFunc(ancestor string, created []string) (func(*os.File) error, error) {
	fi, err := os.Stat(ancestor)
	if err != nil {
		return nil, nil
	}
	o, need := ownerToRestore(fi, geteuid())
	if !need {
		return nil, nil
	}
	for _, d := range created {
		if err := chownPath(d, o.uid, o.gid); err != nil {
			return nil, fmt.Errorf("setting owner of %s: %w", d, err)
		}
	}
	return func(f *os.File) error { return chownFile(f, o.uid, o.gid) }, nil
}
