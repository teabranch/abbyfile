package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/github"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

// Review Focus #5: a tampered binary must never run.
func TestRemoteInstallVerifiesBeforeExecuting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a #!/bin/sh script")
	}
	dir := chdir(t)
	t.Setenv("HOME", t.TempDir())
	marker := filepath.Join(dir, "executed")
	bin := []byte("#!/bin/sh\ntouch " + marker + "\necho '{\"name\":\"agent\",\"version\":\"1.0.0\"}'\n")
	assetName := fmt.Sprintf("agent-%s-%s", runtime.GOOS, runtime.GOARCH)
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			json.NewEncoder(w).Encode([]github.Release{{TagName: "agent/v1.0.0", Assets: []github.Asset{
				{Name: assetName, BrowserDownloadURL: srvURL + "/dl/bin"},
				{Name: "agent-sha256sums.txt", BrowserDownloadURL: srvURL + "/dl/sums"},
			}}})
		case strings.HasSuffix(r.URL.Path, "/dl/bin"):
			w.Write(bin)
		case strings.HasSuffix(r.URL.Path, "/dl/sums"):
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL
	old := newGitHubClient
	newGitHubClient = func() *github.Client { return &github.Client{HTTPClient: srv.Client(), BaseURL: srv.URL} }
	defer func() { newGitHubClient = old }()

	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Out: os.Stdout, Err: os.Stderr}
	_, err := runRemoteInstall("github.com/o/r/agent", opts)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("the downloaded binary was executed before its checksum was verified")
	}
}

// A bare ref to a dotted repo defaults the agent name to "my.repo", which is
// not a valid file name: refuse before contacting GitHub, and say how to fix it.
func TestRemoteInstallRejectsDefaultedDottedAgentName(t *testing.T) {
	chdir(t)
	t.Setenv("HOME", t.TempDir())
	old := newGitHubClient
	newGitHubClient = func() *github.Client {
		t.Fatal("GitHub must not be contacted for an invalid agent name")
		return nil
	}
	defer func() { newGitHubClient = old }()

	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode), Out: os.Stdout, Err: os.Stderr}
	_, err := runRemoteInstall("github.com/o/my.repo", opts)
	if err == nil || !strings.Contains(err.Error(), `github.com/o/my.repo/<agent>`) {
		t.Fatalf("err = %v, want a hint to name the agent explicitly", err)
	}
}
