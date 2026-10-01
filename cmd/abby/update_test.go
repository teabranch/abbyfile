package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/github"
	"github.com/teabranch/abbyfile/pkg/registry"
)

// Fix round 1, item 5: an invalid ABBY_CONFIG_METHOD must fail runUpdate,
// the same way install/uninstall fail on a bad --config-method/env value,
// rather than being silently ignored.
func TestRunUpdateFailsOnBadConfigMethodEnv(t *testing.T) {
	t.Setenv("ABBY_CONFIG_METHOD", "bogus")
	t.Setenv("HOME", t.TempDir())
	if err := runUpdate(""); err == nil {
		t.Error("expected an error from an invalid ABBY_CONFIG_METHOD")
	}
}

// Final review I-1: a per-agent failure (here, a release without a checksum
// asset) must make `abby update` exit non-zero.
func TestRunUpdateFailsWhenAnAgentFailsToUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a #!/bin/sh script")
	}
	dir := chdir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ABBY_CONFIG_METHOD", "file")
	t.Setenv("PATH", t.TempDir())

	assetName := fmt.Sprintf("agent-%s-%s", runtime.GOOS, runtime.GOARCH)
	var srvURL string
	release := func() github.Release {
		return github.Release{TagName: "agent/v2.0.0", Assets: []github.Asset{
			{Name: assetName, BrowserDownloadURL: srvURL + "/dl/bin"},
		}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			json.NewEncoder(w).Encode([]github.Release{release()})
		case strings.Contains(r.URL.Path, "/releases/tags/"):
			json.NewEncoder(w).Encode(release())
		case strings.HasSuffix(r.URL.Path, "/dl/bin"):
			w.Write([]byte("#!/bin/sh\necho '{}'\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL
	old := newGitHubClient
	newGitHubClient = func() *github.Client { return &github.Client{HTTPClient: srv.Client(), BaseURL: srv.URL} }
	defer func() { newGitHubClient = old }()

	regPath, err := registry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	reg.Set(registry.Entry{Name: "agent", Source: "github.com/o/r/agent", Version: "1.0.0",
		Path: filepath.Join(dir, "bin", "agent"), Scope: "local"})
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"", "agent"} {
		err := runUpdate(name)
		if err == nil || !strings.Contains(err.Error(), "1 agent(s) failed to update") {
			t.Errorf("runUpdate(%q) err = %v, want 1 agent(s) failed to update", name, err)
		}
	}
}
