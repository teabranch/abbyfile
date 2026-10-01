package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// BackupSuffix is appended to a config file's path for abby's one-time backup.
const BackupSuffix = ".abbyfile.bak"

// ErrChangedOnDisk means the file changed between ReadSnapshot and Commit
// (for example a running runtime rewrote it). Callers re-plan once, then give up.
var ErrChangedOnDisk = errors.New("file changed on disk since it was read")

// Snapshot is a config file's content at read time, used to write it back
// safely: symlinks are followed to their target, the mode is preserved
// (0600 for new files), a one-time backup is made, and a concurrent change
// is detected before the atomic rename.
type Snapshot struct {
	Path   string // path as given
	Target string // symlink-resolved path that Commit writes
	Exists bool
	Data   []byte      // content at read time (nil when !Exists)
	Mode   fs.FileMode // permission bits to write with
}

// ReadSnapshot reads path (following a symlink to its target). A missing
// file is not an error; a dangling symlink is.
func ReadSnapshot(path string) (*Snapshot, error) {
	target := path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("%s is a symlink whose target cannot be resolved: %w", path, err)
		}
		target = resolved
	}
	s := &Snapshot{Path: path, Target: target, Mode: 0o600}
	data, err := os.ReadFile(target)
	switch {
	case err == nil:
		fi, statErr := os.Stat(target)
		if statErr != nil {
			return nil, statErr
		}
		s.Exists, s.Data, s.Mode = true, data, fi.Mode().Perm()
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return s, nil
}

// Commit atomically replaces the target with data. If the file existed and
// no backup exists yet, it first copies the original to Target+BackupSuffix
// (same mode) and returns that path; otherwise backupPath is "". Returns
// ErrChangedOnDisk, writing nothing, if the file no longer matches Data.
// When running as root and the existing file belongs to another user, the
// new file and the backup are chowned to that user before their renames; a
// file that didn't exist takes the owner of its nearest existing directory.
func (s *Snapshot) Commit(data []byte) (backupPath string, err error) {
	current, readErr := os.ReadFile(s.Target)
	switch {
	case readErr == nil && (!s.Exists || !bytes.Equal(current, s.Data)):
		return "", fmt.Errorf("%s: %w", s.Path, ErrChangedOnDisk)
	case readErr != nil && !errors.Is(readErr, fs.ErrNotExist):
		return "", fmt.Errorf("re-reading %s: %w", s.Path, readErr)
	case readErr != nil && s.Exists:
		return "", fmt.Errorf("%s: %w (it was deleted)", s.Path, ErrChangedOnDisk)
	}
	ancestor, missing := missingDirs(filepath.Dir(s.Target))
	if err := os.MkdirAll(filepath.Dir(s.Target), 0o755); err != nil {
		return "", fmt.Errorf("creating directory for %s: %w", s.Path, err)
	}
	// Under root, keep a user's file (and its backup) owned by that user; a
	// new file, and any directories just created for it, go to the owner of
	// the directory they were created in.
	var restoreOwner func(*os.File) error
	if s.Exists {
		restoreOwner = restoreOwnerFunc(s.Target)
	} else {
		var err error
		if restoreOwner, err = inheritOwnerFunc(ancestor, missing); err != nil {
			return "", err
		}
	}
	if s.Exists {
		bp := s.Target + BackupSuffix
		if _, statErr := os.Stat(bp); statErr != nil {
			if !errors.Is(statErr, fs.ErrNotExist) {
				return "", fmt.Errorf("checking backup %s: %w", bp, statErr)
			}
			if err := writeAtomic(bp, s.Data, s.Mode, restoreOwner); err != nil {
				return "", fmt.Errorf("writing backup %s: %w", bp, err)
			}
			backupPath = bp
		}
	}
	if err := writeAtomic(s.Target, data, s.Mode, restoreOwner); err != nil {
		return backupPath, fmt.Errorf("writing %s: %w", s.Path, err)
	}
	return backupPath, nil
}

// missingDirs returns the nearest existing ancestor of dir (dir itself when
// it exists) and the directories below it that don't exist yet, outermost
// first.
func missingDirs(dir string) (ancestor string, missing []string) {
	for {
		if _, err := os.Stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		missing = append([]string{dir}, missing...)
		dir = parent
	}
	return dir, missing
}
