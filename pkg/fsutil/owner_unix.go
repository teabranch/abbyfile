//go:build unix

package fsutil

import (
	"io/fs"
	"os"
	"syscall"
)

// geteuid and chownFile are replaced in tests.
var (
	geteuid   = os.Geteuid
	chownFile = func(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }
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
