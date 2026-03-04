package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/agentfile/pkg/tools"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	return newTestStoreWithLimits(t, Limits{})
}

func newTestStoreWithLimits(t *testing.T, limits Limits) *FileStore {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "memory")
	store, err := NewFileStoreAt(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestFileStore_WriteRead(t *testing.T) {
	store := newTestStore(t)

	if err := store.Write("greeting", "Hello, world!"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Read("greeting")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Hello, world!" {
		t.Errorf("Read() = %q, want %q", got, "Hello, world!")
	}
}

func TestFileStore_Read_NotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Read("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain 'not found'", err.Error())
	}
}

func TestFileStore_Append(t *testing.T) {
	store := newTestStore(t)

	store.Write("log", "line 1\n")
	store.Append("log", "line 2\n")

	got, err := store.Read("log")
	if err != nil {
		t.Fatal(err)
	}
	if got != "line 1\nline 2\n" {
		t.Errorf("Read() = %q, want %q", got, "line 1\nline 2\n")
	}
}

func TestFileStore_Delete(t *testing.T) {
	store := newTestStore(t)

	store.Write("temp", "data")
	if err := store.Delete("temp"); err != nil {
		t.Fatal(err)
	}

	_, err := store.Read("temp")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestFileStore_Delete_Nonexistent(t *testing.T) {
	store := newTestStore(t)

	if err := store.Delete("ghost"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestFileStore_Keys(t *testing.T) {
	store := newTestStore(t)

	store.Write("alpha", "a")
	store.Write("beta", "b")
	store.Write("gamma", "c")

	keys, err := store.Keys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("Keys() count = %d, want 3", len(keys))
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !keySet[want] {
			t.Errorf("Keys() missing %q", want)
		}
	}
}

func TestManager_Concurrency(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			mgr.Set("key", "value")
			mgr.Get("key")
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestManager_Tools(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	memTools := mgr.Tools()
	if len(memTools) != 4 {
		t.Fatalf("Tools() count = %d, want 4", len(memTools))
	}

	names := make(map[string]bool)
	for _, tool := range memTools {
		names[tool.Name] = true
	}

	for _, want := range []string{"memory_read", "memory_write", "memory_list", "memory_delete"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestManager_HandleWrite_Read(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	memTools := mgr.Tools()

	toolMap := make(map[string]*tools.Definition)
	for _, tool := range memTools {
		toolMap[tool.Name] = tool
	}

	// Write
	result, err := toolMap["memory_write"].Handler(map[string]any{
		"key":   "test",
		"value": "hello world",
	})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if !strings.Contains(result, "Stored") {
		t.Errorf("write result = %q, want it to contain 'Stored'", result)
	}

	// Read
	result, err = toolMap["memory_read"].Handler(map[string]any{"key": "test"})
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("read result = %q, want %q", result, "hello world")
	}

	// List
	result, err = toolMap["memory_list"].Handler(map[string]any{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(result, "test") {
		t.Errorf("list result = %q, want it to contain 'test'", result)
	}

	// Delete
	result, err = toolMap["memory_delete"].Handler(map[string]any{"key": "test"})
	if err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if !strings.Contains(result, "Deleted") {
		t.Errorf("delete result = %q, want it to contain 'Deleted'", result)
	}

	// Verify deleted
	_, err = toolMap["memory_read"].Handler(map[string]any{"key": "test"})
	if err == nil {
		t.Fatal("expected error reading deleted key")
	}
}

func TestFileStore_RejectsPathSeparators(t *testing.T) {
	store := newTestStore(t)

	for _, key := range []string{"foo/bar", "a\\b", "", "sub/dir/key"} {
		if err := store.Write(key, "data"); err == nil {
			t.Errorf("Write(%q) should have failed", key)
		}
		if _, err := store.Read(key); err == nil {
			t.Errorf("Read(%q) should have failed", key)
		}
		if err := store.Append(key, "data"); err == nil {
			t.Errorf("Append(%q) should have failed", key)
		}
		if err := store.Delete(key); err == nil {
			t.Errorf("Delete(%q) should have failed", key)
		}
	}
}

func TestManager_FormatKeysAsContext(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	if got := mgr.FormatKeysAsContext(); got != "" {
		t.Errorf("FormatKeysAsContext() = %q, want empty", got)
	}

	mgr.Set("facts", "some facts")
	mgr.Set("prefs", "some prefs")

	got := mgr.FormatKeysAsContext()
	if got == "" {
		t.Fatal("FormatKeysAsContext() returned empty string")
	}
	if !strings.Contains(got, "facts") || !strings.Contains(got, "prefs") {
		t.Errorf("FormatKeysAsContext() = %q, want it to contain 'facts' and 'prefs'", got)
	}
}

func TestFileStore_MaxKeys(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{MaxKeys: 2})

	if err := store.Write("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("b", "2"); err != nil {
		t.Fatal(err)
	}
	// Third key should fail.
	if err := store.Write("c", "3"); err == nil {
		t.Fatal("expected error when exceeding MaxKeys")
	}
}

func TestFileStore_MaxValueBytes(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{MaxValueBytes: 10})

	if err := store.Write("small", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("big", "this is way too long"); err == nil {
		t.Fatal("expected error when exceeding MaxValueBytes")
	}
}

func TestFileStore_MaxTotalBytes(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{MaxTotalBytes: 20})

	if err := store.Write("a", "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("b", "1234567890"); err != nil {
		t.Fatal(err)
	}
	// This would put us at 30 bytes total.
	if err := store.Write("c", "1234567890"); err == nil {
		t.Fatal("expected error when exceeding MaxTotalBytes")
	}
}

func TestFileStore_LimitsOverwriteAllowed(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{MaxKeys: 1})

	if err := store.Write("only", "first"); err != nil {
		t.Fatal(err)
	}
	// Overwriting the same key should work even at max keys.
	if err := store.Write("only", "second"); err != nil {
		t.Fatalf("overwrite should be allowed: %v", err)
	}
	got, err := store.Read("only")
	if err != nil {
		t.Fatal(err)
	}
	if got != "second" {
		t.Errorf("Read() = %q, want %q", got, "second")
	}
}

func TestFileStore_AppendLimits(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{MaxValueBytes: 10})

	if err := store.Write("log", "hello"); err != nil {
		t.Fatal(err)
	}
	// Appending should check resulting size: 5 + 6 = 11 > 10.
	if err := store.Append("log", " world"); err == nil {
		t.Fatal("expected error when append would exceed MaxValueBytes")
	}
	// Small append should work: 5 + 4 = 9 <= 10.
	if err := store.Append("log", " ok!"); err != nil {
		t.Fatalf("small append should work: %v", err)
	}
}

func TestFileStore_ZeroLimitsUnlimited(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{})

	// Write many keys with large values — should all succeed.
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		if err := store.Write(key, strings.Repeat("x", 1000)); err != nil {
			t.Fatalf("Write(%q) with zero limits: %v", key, err)
		}
	}
}
