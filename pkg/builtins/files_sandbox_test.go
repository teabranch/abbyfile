package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/sandbox"
)

type fixture struct{ root, outside string }

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{root: realTempDir(t), outside: realTempDir(t)}
	writeFile(t, filepath.Join(f.root, "in.go"), "package in\n// needle inside\n")
	writeFile(t, filepath.Join(f.root, "sub", "deep.go"), "package sub\n// needle deep\n")
	writeFile(t, filepath.Join(f.outside, "secret.go"), "package secret\n// needle secret\n")
	if err := os.Symlink(filepath.Join(f.outside, "secret.go"), filepath.Join(f.root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.outside, filepath.Join(f.root, "outdir")); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReadFile_OutsideDenied(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	for _, p := range []string{filepath.Join(f.outside, "secret.go"), "link.go", "outdir/secret.go"} {
		if _, err := handleReadFile(ctx, map[string]any{"path": p}); err == nil || !strings.Contains(err.Error(), "outside the allowed directories") {
			t.Errorf("read %q: err = %v, want denial", p, err)
		}
	}
}

// Review Focus #3.
func TestReadFile_RelativeUsesSandboxCwd(t *testing.T) {
	f := newFixture(t) // process cwd is the package dir, not f.root
	got, err := handleReadFile(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"path": "in.go"})
	if err != nil || !strings.Contains(got, "package in") {
		t.Fatalf("relative read = %q, %v", got, err)
	}
}

func TestWriteFile_OutsideDeniedCreatesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	target := filepath.Join(f.outside, "newdir", "x.txt")
	if _, err := handleWriteFile(ctx, map[string]any{"path": target, "content": "x"}); err == nil {
		t.Fatal("write outside must be denied")
	}
	if _, err := os.Stat(filepath.Join(f.outside, "newdir")); !os.IsNotExist(err) {
		t.Fatal("denied write must not create parent directories")
	}
	if _, err := handleWriteFile(ctx, map[string]any{"path": "outdir/new.txt", "content": "x"}); err == nil {
		t.Fatal("write through a symlinked parent must be denied")
	}
}

func TestWriteFile_RelativeCreatesUnderSandboxCwd(t *testing.T) {
	f := newFixture(t)
	if _, err := handleWriteFile(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"path": "made/new.txt", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(f.root, "made", "new.txt")); err != nil || string(b) != "hi" {
		t.Fatalf("file not created under sandbox cwd: %q, %v", b, err)
	}
}

func TestEditFile_SymlinkEscapeDenied(t *testing.T) {
	f := newFixture(t)
	_, err := handleEditFile(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{
		"path": "link.go", "old_string": "secret", "new_string": "pwned",
	})
	if err == nil {
		t.Fatal("edit through escaping symlink must be denied")
	}
	if b, _ := os.ReadFile(filepath.Join(f.outside, "secret.go")); strings.Contains(string(b), "pwned") {
		t.Fatal("outside file was modified")
	}
}

func TestGlobFiles_SkipsEscapes(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	got, err := handleGlobFiles(ctx, map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "in.go") || strings.Contains(got, "link.go") {
		t.Errorf("glob *.go = %q; want in.go, not link.go", got)
	}
	if !strings.Contains(got, "1 entries outside the allowed directories were skipped") {
		t.Errorf("missing skip note: %q", got)
	}
	got, err = handleGlobFiles(ctx, map[string]any{"pattern": "outdir/*.go"})
	if err != nil || strings.Contains(got, "secret.go") {
		t.Errorf("glob through symlinked dir leaked: %q, %v", got, err)
	}
}

// Review Focus #3.
func TestGlobFiles_RelativeOutputPreserved(t *testing.T) {
	f := newFixture(t)
	got, err := handleGlobFiles(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"pattern": "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.SplitN(got, "\n(", 2)[0], "\n")
	want := map[string]bool{"in.go": true, filepath.Join("sub", "deep.go"): true}
	for _, l := range lines {
		if !want[l] {
			t.Errorf("unexpected line %q in %q (must stay relative, no escapes)", l, got)
		}
	}
}

func TestGlobFiles_RootOutsideDenied(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	if _, err := handleGlobFiles(ctx, map[string]any{"pattern": "*.go", "path": f.outside}); err == nil {
		t.Error("glob base outside must be denied")
	}
	if _, err := handleGlobFiles(ctx, map[string]any{"pattern": "../**/*.go"}); err == nil {
		t.Error("glob ** prefix escaping the sandbox must be denied before walking")
	}
}

func TestGrepSearch_SkipsEscapes(t *testing.T) {
	f := newFixture(t)
	got, err := handleGrepSearch(sandboxCtx(t, f.root, sandbox.Config{}), map[string]any{"pattern": "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "secret") {
		t.Errorf("grep leaked outside content: %q", got)
	}
	if !strings.Contains(got, "needle inside") || !strings.Contains(got, "needle deep") {
		t.Errorf("grep missed inside matches: %q", got)
	}
	if !strings.HasPrefix(got, "in.go:") && !strings.Contains(got, "\nin.go:") {
		t.Errorf("grep output must stay relative to the given path: %q", got)
	}
}

func TestGrepSearch_RootOutsideDenied(t *testing.T) {
	f := newFixture(t)
	ctx := sandboxCtx(t, f.root, sandbox.Config{})
	for _, p := range []string{f.outside, "link.go"} {
		if _, err := handleGrepSearch(ctx, map[string]any{"pattern": "needle", "path": p}); err == nil {
			t.Errorf("grep path %q must be denied", p)
		}
	}
}

func TestFileToolAnnotations(t *testing.T) {
	for _, def := range []struct {
		name        string
		destructive bool
		readOnly    bool
	}{
		{"write_file", true, false}, {"edit_file", true, false},
		{"read_file", false, true}, {"glob_files", false, true}, {"grep_search", false, true},
	} {
		tool := toolByName(t, def.name)
		a := tool.Annotations
		if def.destructive && (a.DestructiveHint == nil || !*a.DestructiveHint) {
			t.Errorf("%s: DestructiveHint must be true", def.name)
		}
		if def.readOnly && (!a.ReadOnlyHint || !a.IdempotentHint) {
			t.Errorf("%s: want ReadOnlyHint and IdempotentHint", def.name)
		}
	}
}
