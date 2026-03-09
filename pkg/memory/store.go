// Package memory provides per-agent persistent file-based memory storage.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Limits configures capacity limits for a FileStore.
// Zero values mean unlimited (backward compatible).
type Limits struct {
	MaxKeys       int   `json:"maxKeys,omitempty"`
	MaxValueBytes int64 `json:"maxValueBytes,omitempty"`
	MaxTotalBytes int64 `json:"maxTotalBytes,omitempty"`
}

// FileStore implements file-based key-value storage under ~/.agentfile/<agent>/memory/.
type FileStore struct {
	dir    string
	limits Limits
}

// NewFileStore creates a FileStore for the given agent name.
func NewFileStore(agentName string, limits Limits) (*FileStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}
	dir := filepath.Join(home, ".agentfile", agentName, "memory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating memory directory: %w", err)
	}
	return &FileStore{dir: dir, limits: limits}, nil
}

// NewFileStoreAt creates a FileStore at a specific directory (useful for testing).
func NewFileStoreAt(dir string, limits Limits) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating memory directory: %w", err)
	}
	return &FileStore{dir: dir, limits: limits}, nil
}

// validateKey checks that a memory key is valid for flat file storage.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("memory key cannot be empty")
	}
	if key == "." || key == ".." {
		return fmt.Errorf("memory key %q is not allowed", key)
	}
	if strings.ContainsAny(key, "/\\") {
		return fmt.Errorf("memory key %q must not contain path separators", key)
	}
	return nil
}

// Read returns the content stored under the given key.
func (s *FileStore) Read(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	data, err := os.ReadFile(s.keyPath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("memory key %q not found", key)
		}
		return "", fmt.Errorf("reading memory key %q: %w", key, err)
	}
	return string(data), nil
}

// Write stores content under the given key, overwriting any existing value.
func (s *FileStore) Write(key, content string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := s.checkValueSize(int64(len(content))); err != nil {
		return err
	}
	if err := s.checkKeyCount(key); err != nil {
		return err
	}
	if err := s.checkTotalSize(key, int64(len(content))); err != nil {
		return err
	}
	return os.WriteFile(s.keyPath(key), []byte(content), 0o600)
}

// Append adds content to the end of an existing key, or creates it.
func (s *FileStore) Append(key, content string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	// Check what the resulting size would be.
	existing, readErr := os.ReadFile(s.keyPath(key))
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("reading existing memory key %q: %w", key, readErr)
	}
	newSize := int64(len(existing)) + int64(len(content))

	if err := s.checkValueSize(newSize); err != nil {
		return err
	}
	// If the key doesn't exist yet, check key count.
	if os.IsNotExist(readErr) {
		if err := s.checkKeyCount(key); err != nil {
			return err
		}
	}
	if err := s.checkTotalSize(key, newSize); err != nil {
		return err
	}

	f, err := os.OpenFile(s.keyPath(key), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("appending to memory key %q: %w", key, err)
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// Delete removes a key from the store.
func (s *FileStore) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	err := os.Remove(s.keyPath(key))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting memory key %q: %w", key, err)
	}
	return nil
}

// Keys returns all stored keys.
func (s *FileStore) Keys() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("listing memory keys: %w", err)
	}

	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".md") {
			keys = append(keys, strings.TrimSuffix(name, ".md"))
		}
	}
	return keys, nil
}

func (s *FileStore) keyPath(key string) string {
	return filepath.Join(s.dir, key+".md")
}

// checkValueSize returns an error if the value exceeds MaxValueBytes.
func (s *FileStore) checkValueSize(size int64) error {
	if s.limits.MaxValueBytes > 0 && size > s.limits.MaxValueBytes {
		return fmt.Errorf("value size %d bytes exceeds limit of %d bytes", size, s.limits.MaxValueBytes)
	}
	return nil
}

// checkKeyCount returns an error if adding a new key would exceed MaxKeys.
// Does not error if the key already exists (overwrite is allowed).
func (s *FileStore) checkKeyCount(key string) error {
	if s.limits.MaxKeys <= 0 {
		return nil
	}
	// If key already exists, overwriting is fine.
	if _, err := os.Stat(s.keyPath(key)); err == nil {
		return nil
	}
	keys, err := s.Keys()
	if err != nil {
		return err
	}
	if len(keys) >= s.limits.MaxKeys {
		return fmt.Errorf("key count %d would exceed limit of %d keys", len(keys)+1, s.limits.MaxKeys)
	}
	return nil
}

// checkTotalSize returns an error if the write would exceed MaxTotalBytes.
// key is the key being written; newSize is the full new value size for that key.
func (s *FileStore) checkTotalSize(key string, newSize int64) error {
	if s.limits.MaxTotalBytes <= 0 {
		return nil
	}
	total, err := s.totalSize()
	if err != nil {
		return err
	}
	// Subtract the current size of this key (if it exists) since it will be replaced.
	if data, err := os.ReadFile(s.keyPath(key)); err == nil {
		total -= int64(len(data))
	}
	if total+newSize > s.limits.MaxTotalBytes {
		return fmt.Errorf("total memory size would be %d bytes, exceeding limit of %d bytes", total+newSize, s.limits.MaxTotalBytes)
	}
	return nil
}

// totalSize returns the sum of all stored values in bytes.
func (s *FileStore) totalSize() (int64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}
