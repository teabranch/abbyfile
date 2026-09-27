package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTemp returns a symlink-free temp dir (macOS /var -> /private/var).
func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustNew(t *testing.T, cfg Config, cwd string, ro ...string) *Sandbox {
	t.Helper()
	s, err := New(cfg, cwd, ro...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	root := realTemp(t)
	outside := realTemp(t)
	write(t, filepath.Join(root, "in.txt"), "in")
	write(t, filepath.Join(outside, "secret.txt"), "secret")
	must(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link-out")))
	must(t, os.Symlink(outside, filepath.Join(root, "dir-out")))
	must(t, os.Symlink(filepath.Join(outside, "new.txt"), filepath.Join(root, "dangling")))
	must(t, os.Symlink(filepath.Join(root, "in.txt"), filepath.Join(root, "link-in")))
	evil := root + "-evil"
	must(t, os.Mkdir(evil, 0o755))
	relOut, _ := filepath.Rel(root, filepath.Join(outside, "secret.txt"))

	cfg := Default()
	s := mustNew(t, cfg, root)

	tests := []struct {
		name   string
		path   string
		access Access
		want   string // "" = expect denial
	}{
		{"relative inside", "in.txt", Read, filepath.Join(root, "in.txt")},
		{"absolute inside", filepath.Join(root, "in.txt"), Read, filepath.Join(root, "in.txt")},
		{"dot-dot traversal", relOut, Read, ""},
		{"absolute outside", filepath.Join(outside, "secret.txt"), Read, ""},
		{"file symlink escape", "link-out", Read, ""},
		{"dir symlink escape", "dir-out/secret.txt", Read, ""},
		{"symlink inside", "link-in", Read, filepath.Join(root, "in.txt")},
		{"new file deep", "new/deep/f.txt", Write, filepath.Join(root, "new", "deep", "f.txt")},
		{"new file under symlinked parent", "dir-out/new.txt", Write, ""},
		{"dangling symlink", "dangling", Write, ""},
		{"sibling prefix", filepath.Join(evil, "x"), Read, ""},
		{"root itself", root, Read, root},
		{"cleaned inside", "new/../in.txt", Read, filepath.Join(root, "in.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Resolve(tt.path, tt.access)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("Resolve(%q) = %q, want denial", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolve_DenialNamesAllowedDirs(t *testing.T) {
	root := realTemp(t)
	s := mustNew(t, Default(), root)
	_, err := s.Resolve("/etc/passwd", Read)
	if err == nil || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "allowed_dirs") {
		t.Fatalf("error %v should name %s and sandbox.allowed_dirs", err, root)
	}
}

func TestResolve_EmptyPath(t *testing.T) {
	s := mustNew(t, Default(), realTemp(t))
	if _, err := s.Resolve("", Read); err == nil {
		t.Fatal("empty path must be an error")
	}
}

// Review Focus #1.
func TestResolve_UnresolvedTempDirAllowed(t *testing.T) {
	raw := t.TempDir() // on macOS this is /var/..., a symlink to /private/var/...
	write(t, filepath.Join(raw, "a.txt"), "a")
	s := mustNew(t, Config{AllowedDirs: []string{raw}}, raw)
	if _, err := s.Resolve(filepath.Join(raw, "a.txt"), Read); err != nil {
		t.Fatalf("path given through the unresolved temp dir must be allowed: %v", err)
	}
}

// Review Focus #4.
func TestResolve_ReadOnlyRoots(t *testing.T) {
	root := realTemp(t)
	spill := filepath.Join(realTemp(t), "spill") // does not exist yet
	s := mustNew(t, Default(), root, spill)
	write(t, filepath.Join(spill, "out.txt"), "big")
	if _, err := s.Resolve(filepath.Join(spill, "out.txt"), Read); err != nil {
		t.Fatalf("read from read-only root: %v", err)
	}
	if _, err := s.Resolve(filepath.Join(spill, "out.txt"), Write); err == nil {
		t.Fatal("write to read-only root must be denied")
	}
}

func TestResolve_RootAllowsEverything(t *testing.T) {
	root := realTemp(t)
	s := mustNew(t, Config{AllowedDirs: []string{"/"}}, root)
	if _, err := s.Resolve("/etc/hosts", Read); err != nil {
		t.Fatalf("allowed_dirs [/] must allow any path: %v", err)
	}
}

func TestNewWarnings(t *testing.T) {
	root := realTemp(t)
	t.Setenv("HOME", root)
	s := mustNew(t, Config{
		AllowedDirs: []string{"/", root, "missing-dir"},
		Bash:        BashUnrestricted,
	}, root)
	joined := strings.Join(s.Warnings(), "\n")
	for _, want := range []string{"resolves to /", "home directory", "does not exist", "unrestricted"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
	if w := mustNew(t, Default(), realTemp(t)).Warnings(); len(w) != 0 { // not $HOME
		t.Errorf("default sandbox in a normal dir should not warn: %q", w)
	}
}

func TestNew_RejectsInvalid(t *testing.T) {
	if _, err := New(Config{Bash: "nope"}, realTemp(t)); err == nil {
		t.Fatal("invalid config must fail")
	}
	if _, err := New(Default(), "relative/cwd"); err == nil {
		t.Fatal("relative cwd must fail")
	}
}

func TestCheckCommand(t *testing.T) {
	root := realTemp(t)
	empty := mustNew(t, Default(), root)
	if _, err := empty.CheckCommand("go test ./..."); err == nil || !strings.Contains(err.Error(), "allow_commands is empty") {
		t.Fatalf("empty allowlist error = %v", err)
	}

	s := mustNew(t, Config{AllowCommands: []string{"go test *", "git status"}}, root)
	argv, err := s.CheckCommand(`go test ./... -run 'Test X'`)
	if err != nil {
		t.Fatalf("allowed command refused: %v", err)
	}
	if len(argv) != 5 || argv[4] != "Test X" {
		t.Errorf("argv = %q", argv)
	}
	if _, err := s.CheckCommand("go vet ./..."); err == nil || !strings.Contains(err.Error(), "go test *") {
		t.Fatalf("mismatch error should list the allowlist, got %v", err)
	}
	if _, err := s.CheckCommand("go test ./... | tee out"); err == nil || !strings.Contains(err.Error(), "no shell") {
		t.Fatalf("metacharacter error should explain there is no shell, got %v", err)
	}
}

func TestAccessors(t *testing.T) {
	root := realTemp(t)
	s := mustNew(t, Config{AllowCommands: []string{"ls"}}, root)
	if s.Cwd() != root || len(s.AllowedDirs()) != 1 || s.AllowedDirs()[0] != root {
		t.Errorf("Cwd=%q AllowedDirs=%q", s.Cwd(), s.AllowedDirs())
	}
	if c := s.Config(); c.Bash != BashRestricted || c.AllowCommands[0] != "ls" {
		t.Errorf("Config() = %+v", c)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
