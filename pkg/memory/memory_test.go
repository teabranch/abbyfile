package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teabranch/abbyfile/pkg/tools"
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
	if len(memTools) != 5 {
		t.Fatalf("Tools() count = %d, want 5", len(memTools))
	}

	names := make(map[string]bool)
	for _, tool := range memTools {
		names[tool.Name] = true
	}

	for _, want := range []string{"memory_read", "memory_write", "memory_list", "memory_delete", "memory_search"} {
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

func TestFileStore_MetadataSidecar(t *testing.T) {
	store := newTestStore(t)

	if err := store.Write("notes", "some notes"); err != nil {
		t.Fatal(err)
	}

	meta := store.ReadMetadata("notes")
	if meta.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set after Write")
	}
	if meta.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set after Write")
	}

	// Verify sidecar file exists on disk.
	metaPath := store.metaPath("notes")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading meta file: %v", err)
	}
	var diskMeta Metadata
	if err := json.Unmarshal(data, &diskMeta); err != nil {
		t.Fatalf("unmarshaling meta: %v", err)
	}
	if diskMeta.CreatedAt.IsZero() {
		t.Error("disk metadata CreatedAt is zero")
	}
}

func TestFileStore_MetadataPreservesCreatedAt(t *testing.T) {
	store := newTestStore(t)

	store.Write("key", "v1")
	meta1 := store.ReadMetadata("key")

	time.Sleep(10 * time.Millisecond)
	store.Write("key", "v2")
	meta2 := store.ReadMetadata("key")

	if !meta2.CreatedAt.Equal(meta1.CreatedAt) {
		t.Errorf("CreatedAt changed on overwrite: %v -> %v", meta1.CreatedAt, meta2.CreatedAt)
	}
	if !meta2.UpdatedAt.After(meta1.UpdatedAt) {
		t.Error("UpdatedAt should advance on overwrite")
	}
}

func TestFileStore_AppendUpdatesMetadata(t *testing.T) {
	store := newTestStore(t)

	store.Write("log", "line1\n")
	meta1 := store.ReadMetadata("log")

	time.Sleep(10 * time.Millisecond)
	store.Append("log", "line2\n")
	meta2 := store.ReadMetadata("log")

	if !meta2.CreatedAt.Equal(meta1.CreatedAt) {
		t.Error("CreatedAt should not change on Append")
	}
	if !meta2.UpdatedAt.After(meta1.UpdatedAt) {
		t.Error("UpdatedAt should advance on Append")
	}
}

func TestFileStore_TTLExpiry(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{TTL: 50 * time.Millisecond})

	store.Write("ephemeral", "data")

	// Should be readable immediately.
	val, err := store.Read("ephemeral")
	if err != nil {
		t.Fatalf("immediate read failed: %v", err)
	}
	if val != "data" {
		t.Errorf("got %q, want %q", val, "data")
	}

	// Wait for expiry.
	time.Sleep(60 * time.Millisecond)

	_, err = store.Read("ephemeral")
	if err == nil {
		t.Fatal("expected error after TTL expiry")
	}
	if !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got: %v", err)
	}
}

func TestFileStore_PerKeyTTL(t *testing.T) {
	store := newTestStore(t) // no store-level TTL

	// Write a key, then manually set a short TTL in its metadata.
	store.Write("temp", "value")
	meta := store.ReadMetadata("temp")
	meta.TTL = "50ms"
	store.writeMetadata("temp", meta)

	// Should be readable immediately.
	if _, err := store.Read("temp"); err != nil {
		t.Fatalf("immediate read failed: %v", err)
	}

	time.Sleep(60 * time.Millisecond)

	_, err := store.Read("temp")
	if !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got: %v", err)
	}
}

func TestFileStore_NoMetadataBackwardCompat(t *testing.T) {
	store := newTestStore(t)

	// Write a file directly without metadata (simulating old data).
	os.WriteFile(store.keyPath("legacy"), []byte("old data"), 0o600)

	val, err := store.Read("legacy")
	if err != nil {
		t.Fatalf("reading legacy key: %v", err)
	}
	if val != "old data" {
		t.Errorf("got %q, want %q", val, "old data")
	}
}

func TestFileStore_KeysSkipsMetaJSON(t *testing.T) {
	store := newTestStore(t)

	store.Write("alpha", "a")
	store.Write("beta", "b")

	keys, err := store.Keys()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if strings.Contains(k, "meta.json") {
			t.Errorf("Keys() should not include meta files, got %q", k)
		}
	}
	if len(keys) != 2 {
		t.Errorf("Keys() count = %d, want 2", len(keys))
	}
}

func TestFileStore_TotalSizeSkipsMetaJSON(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{MaxTotalBytes: 100})

	store.Write("a", "hello") // 5 bytes data + metadata sidecar

	total, err := store.totalSize()
	if err != nil {
		t.Fatal(err)
	}
	// totalSize should only count the .md file (5 bytes), not the .meta.json.
	if total != 5 {
		t.Errorf("totalSize() = %d, want 5", total)
	}
}

func TestFileStore_DeleteRemovesSidecar(t *testing.T) {
	store := newTestStore(t)

	store.Write("temp", "data")
	metaPath := store.metaPath("temp")
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatal("expected meta file to exist after write")
	}

	store.Delete("temp")
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("expected meta file to be removed after delete")
	}
}

func TestFileStore_Search(t *testing.T) {
	store := newTestStore(t)

	store.Write("notes", "line one\nfind me here\nline three")
	store.Write("log", "other stuff\nalso find me\n")
	store.Write("empty", "nothing relevant")

	results, err := store.Search("find me")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, want 2", len(results))
	}

	foundKeys := map[string]bool{}
	for _, r := range results {
		foundKeys[r.Key] = true
		if !strings.Contains(r.Line, "find me") {
			t.Errorf("result line %q does not contain search pattern", r.Line)
		}
	}
	if !foundKeys["notes"] || !foundKeys["log"] {
		t.Errorf("expected results from 'notes' and 'log', got keys: %v", foundKeys)
	}
}

func TestFileStore_SearchNoResults(t *testing.T) {
	store := newTestStore(t)

	store.Write("data", "some content")

	results, err := store.Search("nonexistent pattern")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("Search() returned %d results, want 0", len(results))
	}
}

func TestManager_Search(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	mgr.Set("doc", "important info\nfind this line\nother")

	results, err := mgr.Search("find this")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, want 1", len(results))
	}
	if results[0].Key != "doc" {
		t.Errorf("result key = %q, want %q", results[0].Key, "doc")
	}
}

func TestManager_SearchTool(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	mgr.Set("data", "hello world\nfoo bar baz")

	memTools := mgr.Tools()
	var searchTool *tools.Definition
	for _, tool := range memTools {
		if tool.Name == "memory_search" {
			searchTool = tool
			break
		}
	}
	if searchTool == nil {
		t.Fatal("memory_search tool not found")
	}

	result, err := searchTool.Handler(map[string]any{"pattern": "foo bar"})
	if err != nil {
		t.Fatalf("search tool error: %v", err)
	}
	if !strings.Contains(result, "foo bar baz") {
		t.Errorf("search result = %q, want it to contain 'foo bar baz'", result)
	}
}

func TestManager_GC(t *testing.T) {
	store := newTestStoreWithLimits(t, Limits{TTL: 50 * time.Millisecond})
	mgr := NewManager(store)

	mgr.Set("short-lived", "data1")
	mgr.Set("also-short", "data2")

	// Immediately, GC should not delete anything.
	count, err := mgr.GC()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("GC() deleted %d keys immediately, want 0", count)
	}

	time.Sleep(60 * time.Millisecond)

	count, err = mgr.GC()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("GC() deleted %d keys, want 2", count)
	}

	keys, _ := mgr.Keys()
	if len(keys) != 0 {
		t.Errorf("after GC, %d keys remain, want 0", len(keys))
	}
}

func TestManager_GC_NoTTL(t *testing.T) {
	store := newTestStore(t) // no TTL
	mgr := NewManager(store)

	mgr.Set("permanent", "data")

	count, err := mgr.GC()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("GC() deleted %d keys with no TTL, want 0", count)
	}
}

func TestManager_FormatSummaryAsContext(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	// Empty store.
	if got := mgr.FormatSummaryAsContext(1000); got != "" {
		t.Errorf("FormatSummaryAsContext() on empty = %q, want empty", got)
	}

	mgr.Set("short", "hello")
	mgr.Set("long", strings.Repeat("x", 300))

	got := mgr.FormatSummaryAsContext(10000)
	if !strings.Contains(got, "short: hello") {
		t.Errorf("expected 'short: hello' in output, got %q", got)
	}
	if !strings.Contains(got, "[truncated]") {
		t.Errorf("expected '[truncated]' for long value, got %q", got)
	}
}

func TestManager_FormatSummaryAsContext_MaxBytes(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	for i := 0; i < 10; i++ {
		mgr.Set(fmt.Sprintf("key%d", i), strings.Repeat("a", 50))
	}

	got := mgr.FormatSummaryAsContext(100)
	// Should be truncated — not all 10 keys fit in 100 bytes.
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) >= 10 {
		t.Errorf("FormatSummaryAsContext should truncate, got %d lines", len(lines))
	}
	if len(got) > 100 {
		t.Errorf("output length %d exceeds maxTotalBytes 100", len(got))
	}
}

func TestManager_Tools_Count(t *testing.T) {
	store := newTestStore(t)
	mgr := NewManager(store)

	memTools := mgr.Tools()
	if len(memTools) != 5 {
		t.Fatalf("Tools() count = %d, want 5", len(memTools))
	}

	names := make(map[string]bool)
	for _, tool := range memTools {
		names[tool.Name] = true
	}

	for _, want := range []string{"memory_read", "memory_write", "memory_list", "memory_delete", "memory_search"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}
