package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// skipIfRoot skips the test if running as root (permission-based tests won't work).
func skipIfRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test requires non-root user")
	}
}

func TestSnapshotNewFileGets0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "cfg.json")
	s, err := ReadSnapshot(p)
	if err != nil || s.Exists {
		t.Fatalf("ReadSnapshot = %+v, %v", s, err)
	}
	backup, err := s.Commit([]byte("{}\n"))
	if err != nil || backup != "" {
		t.Fatalf("Commit = %q, %v (no backup expected for a new file)", backup, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSnapshotPreservesModeAndBacksUpOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o640)
	s, _ := ReadSnapshot(p)
	backup, err := s.Commit([]byte("v2"))
	if err != nil || backup != p+BackupSuffix {
		t.Fatalf("Commit = %q, %v", backup, err)
	}
	if b, _ := os.ReadFile(backup); string(b) != "v1" {
		t.Errorf("backup = %q, want v1", b)
	}
	if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o640 {
		t.Errorf("backup mode = %v, want source mode 0640", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("file mode = %v, want preserved 0640", fi.Mode().Perm())
	}
	s2, _ := ReadSnapshot(p)
	if b2, err := s2.Commit([]byte("v3")); err != nil || b2 != "" {
		t.Fatalf("second Commit = %q, %v (backup must not be overwritten or reported)", b2, err)
	}
	if b, _ := os.ReadFile(backup); string(b) != "v1" {
		t.Errorf("backup overwritten: %q", b)
	}
}

// Review Focus #2.
func TestSnapshotCommitThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "claude.json")
	os.MkdirAll(filepath.Dir(real), 0o755)
	os.WriteFile(real, []byte("old"), 0o600)
	link := filepath.Join(dir, ".claude.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSnapshot(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}
	if b, _ := os.ReadFile(real); string(b) != "new" {
		t.Errorf("target = %q, want new", b)
	}
	if _, err := os.Stat(real + BackupSuffix); err != nil {
		t.Errorf("backup should sit next to the target: %v", err)
	}
}

func TestSnapshotDanglingSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "cfg.json")
	os.Symlink(filepath.Join(dir, "missing", "x.json"), link)
	if _, err := ReadSnapshot(link); err == nil {
		t.Fatal("dangling symlink must be refused")
	}
}

func TestSnapshotDetectsConcurrentWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	s, _ := ReadSnapshot(p)
	os.WriteFile(p, []byte("someone else"), 0o600)
	if _, err := s.Commit([]byte("v2")); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "someone else" {
		t.Errorf("file clobbered: %q", b)
	}
}

func TestSnapshotDetectsFileDeleted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	s, _ := ReadSnapshot(p)
	os.Remove(p)
	if _, err := s.Commit([]byte("v2")); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk for deleted file", err)
	}
}

func TestSnapshotPreservesBackupMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("original"), 0o700)
	s, _ := ReadSnapshot(p)
	bp, err := s.Commit([]byte("modified"))
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(bp); fi.Mode().Perm() != 0o700 {
		t.Errorf("backup mode = %o, want 0700", fi.Mode().Perm())
	}
}

func TestSnapshotNewFileInDeepDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "deep", "nested", "dir", "config.json")
	s, err := ReadSnapshot(p)
	if err != nil || s.Exists {
		t.Fatalf("ReadSnapshot = %+v, %v", s, err)
	}
	backup, err := s.Commit([]byte("{}"))
	if err != nil || backup != "" {
		t.Fatalf("Commit = %q, %v (no backup expected for a new file)", backup, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{}" {
		t.Errorf("file content = %q, want {}", b)
	}
}

func TestSnapshotReadDirectory(t *testing.T) {
	dir := t.TempDir()
	s, err := ReadSnapshot(dir)
	if err == nil {
		t.Fatalf("ReadSnapshot on directory should error, got %+v", s)
	}
}

func TestSnapshotReadSymlinkToDirectory(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	os.Mkdir(realDir, 0o755)
	link := filepath.Join(dir, "link")
	os.Symlink(realDir, link)
	s, err := ReadSnapshot(link)
	if err == nil {
		t.Fatalf("ReadSnapshot on symlink to directory should error, got %+v", s)
	}
}

func TestSnapshotFileAppearsBeforeCommit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	s, err := ReadSnapshot(p)
	if err != nil || s.Exists {
		t.Fatalf("ReadSnapshot = %+v, %v", s, err)
	}
	// File appears between ReadSnapshot and Commit
	os.WriteFile(p, []byte("someone was here"), 0o600)
	if _, err := s.Commit([]byte("v1")); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk when file appears", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "someone was here" {
		t.Errorf("file clobbered: %q", b)
	}
}

func TestSnapshotDetectsConcurrentWriteNoBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	s, _ := ReadSnapshot(p)
	os.WriteFile(p, []byte("someone else"), 0o600)
	if _, err := s.Commit([]byte("v2")); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "someone else" {
		t.Errorf("file clobbered: %q", b)
	}
	// Verify no backup was created
	if _, err := os.Stat(p + BackupSuffix); err == nil {
		t.Errorf("backup should not be created on concurrent write")
	}
}

func TestSnapshotDeletedFileNoBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(p, []byte("v1"), 0o600)
	s, _ := ReadSnapshot(p)
	os.Remove(p)
	if _, err := s.Commit([]byte("v2")); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk for deleted file", err)
	}
	// Verify no backup was created
	if _, err := os.Stat(p + BackupSuffix); err == nil {
		t.Errorf("backup should not be created when original file is deleted")
	}
}

func TestSnapshotBackupStatError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	os.WriteFile(p, []byte("original"), 0o600)
	s, _ := ReadSnapshot(p)

	// Create a symlink loop for the backup path to make Stat fail with ELOOP
	backupPath := p + BackupSuffix
	os.Symlink(backupPath, backupPath)
	defer os.Remove(backupPath) // cleanup

	if _, err := s.Commit([]byte("new")); err == nil {
		t.Errorf("Commit should error when backup stat fails, got nil")
	}
	// Target should be unchanged
	if b, _ := os.ReadFile(p); string(b) != "original" {
		t.Errorf("target should be unchanged on backup stat error, got %q", b)
	}
}

func TestSnapshotMkdirAllFails(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	s, err := ReadSnapshot(p)
	if err != nil || s.Exists {
		t.Fatalf("ReadSnapshot = %+v, %v", s, err)
	}

	// Make parent a regular file so MkdirAll fails
	blockingFile := filepath.Join(dir, "block")
	os.WriteFile(blockingFile, []byte("blocking"), 0o600)
	s.Target = filepath.Join(blockingFile, "subdir", "cfg.json")

	if _, err := s.Commit([]byte("data")); err == nil {
		t.Errorf("Commit should error when MkdirAll fails, got nil")
	}
}

func TestSnapshotWriteTargetPermissionDenied(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	os.WriteFile(p, []byte("original"), 0o600)
	s, _ := ReadSnapshot(p)

	// Remove write permission on parent dir to block WriteAtomic
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o755) // cleanup

	if _, err := s.Commit([]byte("new")); err == nil {
		t.Errorf("Commit should error when WriteAtomic fails, got nil")
	}
	// File should be unchanged
	if b, _ := os.ReadFile(p); string(b) != "original" {
		t.Errorf("file should be unchanged on write failure")
	}
}
