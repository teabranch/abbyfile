package builtins

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sandboxCtx returns a context whose sandbox has cwd = root. A zero cfg
// means sandbox.Default() (allowed_dirs ["."] = root).
func sandboxCtx(t *testing.T, root string, cfg sandbox.Config) context.Context {
	t.Helper()
	sb, err := sandbox.New(cfg, root)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}
	return sandbox.NewContext(context.Background(), sb)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func toolByName(t *testing.T, name string) *tools.Definition {
	t.Helper()
	for _, d := range All() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no builtin %q", name)
	return nil
}
