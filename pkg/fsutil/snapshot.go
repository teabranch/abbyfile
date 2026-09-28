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
	if err := os.MkdirAll(filepath.Dir(s.Target), 0o755); err != nil {
		return "", fmt.Errorf("creating directory for %s: %w", s.Path, err)
	}
	if s.Exists {
		bp := s.Target + BackupSuffix
		if _, statErr := os.Stat(bp); errors.Is(statErr, fs.ErrNotExist) {
			if err := WriteAtomic(bp, s.Data, s.Mode); err != nil {
				return "", fmt.Errorf("writing backup %s: %w", bp, err)
			}
			backupPath = bp
		}
	}
	if err := WriteAtomic(s.Target, data, s.Mode); err != nil {
		return backupPath, fmt.Errorf("writing %s: %w", s.Path, err)
	}
	return backupPath, nil
}
