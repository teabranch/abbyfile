package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(r.Agents) != 0 {
		t.Errorf("expected empty agents, got %d", len(r.Agents))
	}
}

func TestSetGetRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	r.Set(Entry{
		Name:    "test-agent",
		Source:  "local",
		Version: "0.1.0",
		Path:    "/usr/local/bin/test-agent",
		Scope:   "global",
	})

	e, ok := r.Get("test-agent")
	if !ok {
		t.Fatal("Get: entry not found")
	}
	if e.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", e.Version, "0.1.0")
	}
	if e.InstalledAt == "" {
		t.Error("InstalledAt should be set")
	}

	r.Remove("test-agent")
	if _, ok := r.Get("test-agent"); ok {
		t.Error("entry should be removed")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "registry.json")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	r.Set(Entry{
		Name:    "agent-a",
		Source:  "github.com/owner/repo/agent-a",
		Version: "1.2.3",
		Path:    "/home/user/.agentfile/bin/agent-a",
		Scope:   "local",
	})
	r.Set(Entry{
		Name:    "agent-b",
		Source:  "local",
		Version: "0.0.1",
		Path:    "/usr/local/bin/agent-b",
		Scope:   "global",
	})

	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load back from the same path.
	r2, err := Load(path)
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if len(r2.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(r2.Agents))
	}

	e, ok := r2.Get("agent-a")
	if !ok {
		t.Fatal("agent-a not found after reload")
	}
	if e.Source != "github.com/owner/repo/agent-a" {
		t.Errorf("Source = %q, want github ref", e.Source)
	}
}

func TestSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	r, _ := Load(path)
	r.Set(Entry{Name: "x", Source: "local", Version: "1.0.0", Path: "/bin/x", Scope: "local"})
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Temp file should not remain.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up after atomic save")
	}
}

func TestList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	r, _ := Load(path)
	r.Set(Entry{Name: "a", Source: "local", Version: "1.0.0", Path: "/a", Scope: "local"})
	r.Set(Entry{Name: "b", Source: "local", Version: "2.0.0", Path: "/b", Scope: "global"})

	entries := r.List()
	if len(entries) != 2 {
		t.Errorf("List: expected 2 entries, got %d", len(entries))
	}
}
