// Package github provides a client for downloading agent binaries
// from GitHub Releases.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ReleaseRef is a parsed reference to a GitHub release.
type ReleaseRef struct {
	Owner   string // repository owner
	Repo    string // repository name
	Agent   string // agent name (may differ from repo)
	Version string // specific version or "" for latest
}

// Release describes a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset describes a release asset (binary).
type Asset struct {
	Name               string `json:"name"`
	URL                string `json:"url"`                  // API URL (auth-safe for private repos)
	BrowserDownloadURL string `json:"browser_download_url"` // CDN URL (strips auth on redirect)
}

// Client accesses GitHub Releases.
type Client struct {
	HTTPClient *http.Client
	BaseURL    string // defaults to "https://api.github.com"
	Token      string // optional, for private repos / rate limits
}

// NewClient creates a client. It reads GITHUB_TOKEN from the environment,
// falling back to `gh auth token` if the gh CLI is available.
func NewClient() *Client {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	return &Client{
		HTTPClient: http.DefaultClient,
		BaseURL:    "https://api.github.com",
		Token:      token,
	}
}

// ParseRef parses a reference like "github.com/owner/repo/agent@version".
// The agent segment defaults to the repo name if omitted.
// The version segment is optional (omit @version for latest).
func ParseRef(ref string) (ReleaseRef, error) {
	ref = strings.TrimPrefix(ref, "https://")

	if !strings.HasPrefix(ref, "github.com/") {
		return ReleaseRef{}, fmt.Errorf("expected github.com/ prefix, got %q", ref)
	}

	ref = strings.TrimPrefix(ref, "github.com/")

	// Split off @version if present.
	var version string
	if idx := strings.LastIndex(ref, "@"); idx >= 0 {
		version = strings.TrimPrefix(ref[idx+1:], "v")
		ref = ref[:idx]
	}

	parts := strings.Split(ref, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return ReleaseRef{}, fmt.Errorf("expected owner/repo[/agent], got %q", ref)
	}

	r := ReleaseRef{
		Owner:   parts[0],
		Repo:    parts[1],
		Version: version,
	}
	if len(parts) == 3 {
		r.Agent = parts[2]
	} else {
		r.Agent = parts[1] // default agent name = repo name
	}
	return r, nil
}

// IsRemoteRef returns true if ref looks like a GitHub reference.
func IsRemoteRef(ref string) bool {
	return strings.HasPrefix(ref, "github.com/") || strings.HasPrefix(ref, "https://github.com/")
}

// LatestRelease finds the latest release for the given agent.
// For multi-agent repos (tag format: <agent>/v<version>), it filters by agent prefix.
func (c *Client) LatestRelease(ctx context.Context, ref ReleaseRef) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases", c.BaseURL, ref.Owner, ref.Repo)
	data, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}

	var releases []Release
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, fmt.Errorf("parsing releases: %w", err)
	}

	prefix := ref.Agent + "/v"
	for _, r := range releases {
		if strings.HasPrefix(r.TagName, prefix) {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("no release found for agent %q", ref.Agent)
}

// GetRelease fetches a specific release by tag (<agent>/v<version>).
func (c *Client) GetRelease(ctx context.Context, ref ReleaseRef) (*Release, error) {
	tag := ref.Agent + "/v" + ref.Version
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.BaseURL, ref.Owner, ref.Repo, tag)
	data, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}

	var release Release
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, fmt.Errorf("parsing release: %w", err)
	}
	return &release, nil
}

// DownloadAsset downloads a release asset to the given writer.
// When a token is set and the asset has an API URL, it uses the API URL
// to avoid auth-stripping redirects that break private repo downloads.
func (c *Client) DownloadAsset(ctx context.Context, asset Asset, w io.Writer) error {
	// Use API URL when authenticated (private repos redirect CDN URLs
	// through S3, and Go's HTTP client strips the Authorization header
	// on cross-domain redirects).
	dlURL := asset.BrowserDownloadURL
	if c.Token != "" && asset.URL != "" {
		dlURL = asset.URL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "token "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max for error bodies
		return fmt.Errorf("download failed (HTTP %d): %s", resp.StatusCode, body)
	}

	_, err = io.Copy(w, io.LimitReader(resp.Body, 500<<20)) // 500 MB max for asset downloads
	return err
}

// ResolveAssetName returns the expected binary asset name for the current platform.
func ResolveAssetName(agentName string) string {
	return fmt.Sprintf("%s-%s-%s", agentName, runtime.GOOS, runtime.GOARCH)
}

// FindAsset finds the matching asset for the current platform in a release.
func FindAsset(release *Release, agentName string) (*Asset, error) {
	want := ResolveAssetName(agentName)
	for i := range release.Assets {
		if release.Assets[i].Name == want {
			return &release.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("no asset %q found in release %s (available: %s)",
		want, release.TagName, assetNames(release))
}

// VersionFromTag extracts the version from a tag like "agent/v1.2.3".
func VersionFromTag(tag string) string {
	if idx := strings.LastIndex(tag, "/v"); idx >= 0 {
		return tag[idx+2:]
	}
	return strings.TrimPrefix(tag, "v")
}

func assetNames(r *Release) string {
	names := make([]string, len(r.Assets))
	for i, a := range r.Assets {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "token "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max for API responses
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error (HTTP %d): %s", resp.StatusCode, body)
	}
	return body, nil
}
