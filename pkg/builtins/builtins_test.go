package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForNames(t *testing.T) {
	defs, err := ForNames([]string{"Read", "Write", "Bash"})
	if err != nil {
		t.Fatalf("ForNames: %v", err)
	}
	if len(defs) != 3 {
		t.Fatalf("got %d defs, want 3", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	for _, want := range []string{"read_file", "write_file", "run_command"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestForNames_Unknown(t *testing.T) {
	_, err := ForNames([]string{"Read", "Unknown"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "Unknown") {
		t.Errorf("error = %q, want to contain 'Unknown'", err)
	}
}

func TestAll(t *testing.T) {
	defs := All()
	if len(defs) != 6 {
		t.Errorf("All() returned %d defs, want 6", len(defs))
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	result, err := handleReadFile(map[string]any{"path": path})
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestReadFile_NotFound(t *testing.T) {
	_, err := handleReadFile(map[string]any{"path": "/nonexistent/file"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.txt")

	result, err := handleWriteFile(map[string]any{
		"path":    path,
		"content": "written content",
	})
	if err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	if !strings.Contains(result, "15 bytes") {
		t.Errorf("result = %q, want to contain byte count", result)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "written content" {
		t.Errorf("file content = %q", string(data))
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	_, err := handleEditFile(map[string]any{
		"path":       path,
		"old_string": "world",
		"new_string": "Go",
	})
	if err != nil {
		t.Fatalf("editFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello Go" {
		t.Errorf("file content = %q, want %q", string(data), "hello Go")
	}
}

func TestEditFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	_, err := handleEditFile(map[string]any{
		"path":       path,
		"old_string": "missing",
		"new_string": "new",
	})
	if err == nil {
		t.Fatal("expected error for missing old_string")
	}
}

func TestEditFile_Duplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("aa bb aa"), 0o644)

	_, err := handleEditFile(map[string]any{
		"path":       path,
		"old_string": "aa",
		"new_string": "cc",
	})
	if err == nil {
		t.Fatal("expected error for duplicate old_string")
	}
}

func TestRunCommand(t *testing.T) {
	result, err := handleRunCommand(map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if strings.TrimSpace(result) != "hello" {
		t.Errorf("result = %q, want %q", strings.TrimSpace(result), "hello")
	}
}

func TestGlobFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte(""), 0o644)

	result, err := handleGlobFiles(map[string]any{
		"pattern": "*.go",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("globFiles: %v", err)
	}
	if !strings.Contains(result, "a.go") || !strings.Contains(result, "b.go") {
		t.Errorf("result = %q, want to contain a.go and b.go", result)
	}
	if strings.Contains(result, "c.txt") {
		t.Errorf("result should not contain c.txt: %q", result)
	}
}

func TestGlobFiles_DoubleStarPattern(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(dir, "root.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(sub, "nested.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(sub, "skip.txt"), []byte(""), 0o644)

	result, err := handleGlobFiles(map[string]any{
		"pattern": "**/*.go",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("globFiles: %v", err)
	}
	if !strings.Contains(result, "root.go") || !strings.Contains(result, "nested.go") {
		t.Errorf("result = %q, want both .go files", result)
	}
}

func TestGrepSearch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("func main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("no match here\n"), 0o644)

	result, err := handleGrepSearch(map[string]any{
		"pattern": "func main",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("grepSearch: %v", err)
	}
	if !strings.Contains(result, "a.go:1:func main()") {
		t.Errorf("result = %q, want match in a.go", result)
	}
}

func TestGrepSearch_WithGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("hello\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello\n"), 0o644)

	result, err := handleGrepSearch(map[string]any{
		"pattern": "hello",
		"path":    dir,
		"glob":    "*.go",
	})
	if err != nil {
		t.Fatalf("grepSearch: %v", err)
	}
	if !strings.Contains(result, "a.go") {
		t.Errorf("result = %q, want match in a.go", result)
	}
	if strings.Contains(result, "b.txt") {
		t.Errorf("result should not contain b.txt: %q", result)
	}
}
