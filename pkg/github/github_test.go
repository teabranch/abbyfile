package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		input   string
		want    ReleaseRef
		wantErr bool
	}{
		{
			input: "github.com/owner/repo/agent@1.0.0",
			want:  ReleaseRef{Owner: "owner", Repo: "repo", Agent: "agent", Version: "1.0.0"},
		},
		{
			input: "github.com/owner/repo/agent@v1.0.0",
			want:  ReleaseRef{Owner: "owner", Repo: "repo", Agent: "agent", Version: "1.0.0"},
		},
		{
			input: "github.com/owner/repo/agent",
			want:  ReleaseRef{Owner: "owner", Repo: "repo", Agent: "agent"},
		},
		{
			input: "github.com/owner/repo@2.0.0",
			want:  ReleaseRef{Owner: "owner", Repo: "repo", Agent: "repo", Version: "2.0.0"},
		},
		{
			input: "github.com/owner/repo",
			want:  ReleaseRef{Owner: "owner", Repo: "repo", Agent: "repo"},
		},
		{
			input:   "gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			input:   "github.com/only-owner",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		got, err := ParseRef(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRef(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRef(%q): %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", tt.input, got, tt.want)
		}
	}
}

func TestIsRemoteRef(t *testing.T) {
	if !IsRemoteRef("github.com/owner/repo") {
		t.Error("expected true for github.com ref")
	}
	if IsRemoteRef("my-agent") {
		t.Error("expected false for local name")
	}
}

func TestGetRelease(t *testing.T) {
	release := Release{
		TagName: "myagent/v1.0.0",
		Assets: []Asset{
			{Name: "myagent-linux-amd64", BrowserDownloadURL: "https://example.com/myagent-linux-amd64"},
			{Name: "myagent-darwin-arm64", BrowserDownloadURL: "https://example.com/myagent-darwin-arm64"},
		},
	}
	releaseJSON, _ := json.Marshal(release)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/tags/myagent/v1.0.0" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Write(releaseJSON)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	ref := ReleaseRef{Owner: "owner", Repo: "repo", Agent: "myagent", Version: "1.0.0"}

	got, err := c.GetRelease(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got.TagName != "myagent/v1.0.0" {
		t.Errorf("TagName = %q, want %q", got.TagName, "myagent/v1.0.0")
	}
	if len(got.Assets) != 2 {
		t.Errorf("expected 2 assets, got %d", len(got.Assets))
	}
}

func TestLatestRelease(t *testing.T) {
	releases := []Release{
		{TagName: "other/v2.0.0"},
		{TagName: "myagent/v1.1.0", Assets: []Asset{{Name: "myagent-linux-amd64"}}},
		{TagName: "myagent/v1.0.0"},
	}
	releasesJSON, _ := json.Marshal(releases)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases" {
			http.NotFound(w, r)
			return
		}
		w.Write(releasesJSON)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	ref := ReleaseRef{Owner: "owner", Repo: "repo", Agent: "myagent"}

	got, err := c.LatestRelease(context.Background(), ref)
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if got.TagName != "myagent/v1.1.0" {
		t.Errorf("TagName = %q, want latest %q", got.TagName, "myagent/v1.1.0")
	}
}

func TestLatestReleaseNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	ref := ReleaseRef{Owner: "owner", Repo: "repo", Agent: "nonexistent"}

	_, err := c.LatestRelease(context.Background(), ref)
	if err == nil {
		t.Error("expected error for no matching release")
	}
}

func TestDownloadAsset(t *testing.T) {
	content := "binary-content-here"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	var buf strings.Builder
	asset := Asset{
		Name:               "test-asset",
		BrowserDownloadURL: srv.URL + "/asset",
	}
	err := c.DownloadAsset(context.Background(), asset, &buf)
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if buf.String() != content {
		t.Errorf("got %q, want %q", buf.String(), content)
	}
}

func TestDownloadAssetWithToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL, Token: "ghp_secret"}
	asset := Asset{
		Name:               "test-asset",
		URL:                srv.URL + "/api-asset",
		BrowserDownloadURL: srv.URL + "/cdn-asset",
	}
	err := c.DownloadAsset(context.Background(), asset, io.Discard)
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if gotAuth != "token ghp_secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "token ghp_secret")
	}
}

func TestDownloadAssetUsesAPIURLWithToken(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL, Token: "ghp_secret"}
	asset := Asset{
		Name:               "test-asset",
		URL:                srv.URL + "/api-endpoint",
		BrowserDownloadURL: srv.URL + "/cdn-endpoint",
	}
	err := c.DownloadAsset(context.Background(), asset, io.Discard)
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if gotPath != "/api-endpoint" {
		t.Errorf("expected API URL path /api-endpoint, got %q", gotPath)
	}
}

func TestDownloadAssetUsesBrowserURLWithoutToken(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL} // no token
	asset := Asset{
		Name:               "test-asset",
		URL:                srv.URL + "/api-endpoint",
		BrowserDownloadURL: srv.URL + "/cdn-endpoint",
	}
	err := c.DownloadAsset(context.Background(), asset, io.Discard)
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if gotPath != "/cdn-endpoint" {
		t.Errorf("expected browser URL path /cdn-endpoint, got %q", gotPath)
	}
}

func TestFindAsset(t *testing.T) {
	assetName := ResolveAssetName("myagent")
	release := &Release{
		TagName: "myagent/v1.0.0",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: "https://example.com/" + assetName},
			{Name: "myagent-other-arch", BrowserDownloadURL: "https://example.com/other"},
		},
	}

	asset, err := FindAsset(release, "myagent")
	if err != nil {
		t.Fatalf("FindAsset: %v", err)
	}
	if asset.Name != assetName {
		t.Errorf("Name = %q, want %q", asset.Name, assetName)
	}
}

func TestFindAssetNotFound(t *testing.T) {
	release := &Release{
		TagName: "myagent/v1.0.0",
		Assets:  []Asset{{Name: "myagent-windows-amd64"}},
	}

	_, err := FindAsset(release, "myagent")
	if err == nil {
		t.Error("expected error when asset not found for current platform")
	}
}

func TestResolveAssetName(t *testing.T) {
	got := ResolveAssetName("test-agent")
	want := "test-agent-" + runtime.GOOS + "-" + runtime.GOARCH
	if got != want {
		t.Errorf("ResolveAssetName = %q, want %q", got, want)
	}
}

func TestVersionFromTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"myagent/v1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
	}
	for _, tt := range tests {
		got := VersionFromTag(tt.tag)
		if got != tt.want {
			t.Errorf("VersionFromTag(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestListAgentsMultiAgent(t *testing.T) {
	releases := []Release{
		{TagName: "agent-a/v1.0.0"},
		{TagName: "agent-b/v2.0.0"},
		{TagName: "agent-a/v0.9.0"},
	}
	releasesJSON, _ := json.Marshal(releases)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases" {
			http.NotFound(w, r)
			return
		}
		w.Write(releasesJSON)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	agents, err := c.ListAgents(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d: %v", len(agents), agents)
	}
	if agents[0] != "agent-a" || agents[1] != "agent-b" {
		t.Errorf("agents = %v, want [agent-a, agent-b]", agents)
	}
}

func TestListAgentsSingleAgent(t *testing.T) {
	releases := []Release{
		{TagName: "v1.0.0"},
		{TagName: "v0.9.0"},
	}
	releasesJSON, _ := json.Marshal(releases)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(releasesJSON)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	agents, err := c.ListAgents(context.Background(), "owner", "myrepo")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 || agents[0] != "myrepo" {
		t.Errorf("agents = %v, want [myrepo]", agents)
	}
}

func TestListAgentsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	_, err := c.ListAgents(context.Background(), "owner", "repo")
	if err == nil {
		t.Error("expected error for empty releases")
	}
}

func TestListAgentsPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			releases := []Release{{TagName: "agent-b/v1.0.0"}}
			data, _ := json.Marshal(releases)
			w.Write(data)
			return
		}
		releases := []Release{{TagName: "agent-a/v1.0.0"}}
		data, _ := json.Marshal(releases)
		// Link header pointing to page 2.
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/owner/repo/releases?page=2>; rel="next"`, r.Host))
		// Fix: use full URL with scheme.
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/releases?page=2>; rel="next"`, r.Host))
		w.Write(data)
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	agents, err := c.ListAgents(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d: %v", len(agents), agents)
	}
	if agents[0] != "agent-a" || agents[1] != "agent-b" {
		t.Errorf("agents = %v, want [agent-a, agent-b]", agents)
	}
}

func TestGetWithAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"tag_name":"x/v1.0.0","assets":[]}`))
	}))
	defer srv.Close()

	c := &Client{HTTPClient: srv.Client(), BaseURL: srv.URL, Token: "ghp_test123"}
	ref := ReleaseRef{Owner: "o", Repo: "r", Agent: "x", Version: "1.0.0"}
	_, _ = c.GetRelease(context.Background(), ref)
	if gotAuth != "token ghp_test123" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "token ghp_test123")
	}
}
