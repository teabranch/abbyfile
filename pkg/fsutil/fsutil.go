// Package fsutil provides shared filesystem utility functions.
package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyFile copies src to dst, creating dst if it doesn't exist.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// WriteAtomic writes data to path atomically using a temp-file-then-rename pattern.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, nil)
}

// owner is a file's uid/gid.
type owner struct{ uid, gid int }

// writeAtomic is WriteAtomic with an optional prepare hook, run on the synced
// temp file just before it is closed and renamed into place.
func writeAtomic(path string, data []byte, perm os.FileMode, prepare func(*os.File) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	fail := func(format string, err error) error {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf(format, err)
	}

	if _, err := tmp.Write(data); err != nil {
		return fail("writing temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fail("setting permissions: %w", err)
	}
	if prepare != nil {
		if err := prepare(tmp); err != nil {
			return fail("setting owner: %w", err)
		}
	}
	// Flush to disk before the rename so a crash can't leave an empty or
	// partial file in place of the original.
	if err := tmp.Sync(); err != nil {
		return fail("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// SHA256File computes the SHA256 hash of a file and returns it as a hex string.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
