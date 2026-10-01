//go:build unix

package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fakeFileInfo struct{ sys any }

func (f fakeFileInfo) Name() string       { return "f" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0o600 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return f.sys }

// Final review I-5: only root rewriting someone else's file restores the owner.
func TestOwnerToRestore(t *testing.T) {
	owned := func(uid, gid uint32) fs.FileInfo { return fakeFileInfo{&syscall.Stat_t{Uid: uid, Gid: gid}} }
	cases := []struct {
		name     string
		fi       fs.FileInfo
		euid     int
		wantNeed bool
	}{
		{"root rewriting a user's file", owned(501, 20), 0, true},
		{"root rewriting root's file", owned(0, 0), 0, false},
		{"user rewriting another user's file", owned(502, 20), 501, false},
		{"user rewriting own file", owned(501, 20), 501, false},
		{"no stat info", fakeFileInfo{nil}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, need := ownerToRestore(c.fi, c.euid)
			if need != c.wantNeed {
				t.Fatalf("need = %v, want %v", need, c.wantNeed)
			}
			if need && (o.uid != 501 || o.gid != 20) {
				t.Errorf("owner = %+v, want 501:20", o)
			}
		})
	}
}

// Commit chowns both the rewritten file and the new backup (before their
// renames) when the owner must be restored, and never otherwise.
func TestCommitRestoresOwnerUnderRoot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	fi, _ := os.Stat(p)
	st := fi.Sys().(*syscall.Stat_t)

	var calls []string
	oldEuid, oldChown := geteuid, chownFile
	defer func() { geteuid, chownFile = oldEuid, oldChown }()
	chownFile = func(f *os.File, uid, gid int) error {
		if uid != int(st.Uid) || gid != int(st.Gid) {
			t.Errorf("chown to %d:%d, want %d:%d", uid, gid, st.Uid, st.Gid)
		}
		calls = append(calls, filepath.Dir(f.Name()))
		return nil
	}

	// Pretend to be root when the file is owned by someone else.
	geteuid = func() int {
		if st.Uid == 0 {
			return 1
		}
		return 0
	}
	s, _ := ReadSnapshot(p)
	if _, err := s.Commit([]byte("v2")); err != nil {
		t.Fatal(err)
	}
	if st.Uid != 0 && len(calls) != 2 {
		t.Errorf("chown calls = %v, want 2 (backup and target)", calls)
	}

	// Not root: no chown.
	calls = nil
	geteuid = func() int { return int(st.Uid) }
	s, _ = ReadSnapshot(p)
	if _, err := s.Commit([]byte("v3")); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("chown calls = %v, want none", calls)
	}
}

// A real chown to another user needs root; run it only then.
func TestCommitRestoresOwnerRealChown(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	if err := os.Chown(p, 65534, 65534); err != nil {
		t.Skip(err)
	}
	s, _ := ReadSnapshot(p)
	if _, err := s.Commit([]byte("v2")); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, p + BackupSuffix} {
		fi, _ := os.Stat(f)
		st := fi.Sys().(*syscall.Stat_t)
		if st.Uid != 65534 || st.Gid != 65534 {
			t.Errorf("%s owner = %d:%d, want 65534:65534", f, st.Uid, st.Gid)
		}
	}
}

// A config file (and any directories) that root creates on a user's behalf
// — e.g. `sudo abby install --global` with no ~/.codex yet — belongs to the
// owner of the nearest existing ancestor directory, not to root.
func TestCommitNewFileInheritsAncestorOwnerUnderRoot(t *testing.T) {
	base := t.TempDir()
	fi, _ := os.Stat(base)
	st := fi.Sys().(*syscall.Stat_t)
	if st.Uid == 0 {
		t.Skip("temp dir is owned by root; nothing to inherit")
	}
	p := filepath.Join(base, "a", "b", "cfg.toml")

	var files, dirs []string
	oldEuid, oldChown, oldChownPath := geteuid, chownFile, chownPath
	defer func() { geteuid, chownFile, chownPath = oldEuid, oldChown, oldChownPath }()
	geteuid = func() int { return 0 }
	chownFile = func(f *os.File, uid, gid int) error {
		if uid != int(st.Uid) || gid != int(st.Gid) {
			t.Errorf("chown file to %d:%d, want %d:%d", uid, gid, st.Uid, st.Gid)
		}
		files = append(files, f.Name())
		return nil
	}
	chownPath = func(path string, uid, gid int) error {
		if uid != int(st.Uid) || gid != int(st.Gid) {
			t.Errorf("chown %s to %d:%d, want %d:%d", path, uid, gid, st.Uid, st.Gid)
		}
		dirs = append(dirs, path)
		return nil
	}

	s, _ := ReadSnapshot(p)
	if _, err := s.Commit([]byte("v1")); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("file chowns = %v, want 1 (the new file)", files)
	}
	want := []string{filepath.Join(base, "a"), filepath.Join(base, "a", "b")}
	if len(dirs) != 2 || dirs[0] != want[0] || dirs[1] != want[1] {
		t.Errorf("dir chowns = %v, want %v (only the directories Commit created)", dirs, want)
	}

	// Not root: nothing is chowned.
	files, dirs = nil, nil
	geteuid = func() int { return int(st.Uid) }
	s, _ = ReadSnapshot(filepath.Join(base, "c", "cfg.toml"))
	if _, err := s.Commit([]byte("v1")); err != nil {
		t.Fatal(err)
	}
	if len(files)+len(dirs) != 0 {
		t.Errorf("chowns as non-root = %v %v, want none", files, dirs)
	}
}
