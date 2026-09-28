package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/github"
)

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fakeReleaseServer serves assets by name under /dl/<name>.
func fakeReleaseServer(t *testing.T, assets map[string][]byte) (*github.Client, func(names ...string) *github.Release) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		b, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	client := &github.Client{HTTPClient: srv.Client(), BaseURL: srv.URL}
	return client, func(names ...string) *github.Release {
		rel := &github.Release{TagName: "agent/v1.0.0"}
		for _, n := range names {
			rel.Assets = append(rel.Assets, github.Asset{Name: n, BrowserDownloadURL: srv.URL + "/dl/" + n})
		}
		return rel
	}
}

// Review Focus #5.
func TestVerifyReleaseAsset(t *testing.T) {
	bin := []byte("#!/bin/sh\necho hi\n")
	binPath := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(binPath, bin, 0o644)
	asset := github.Asset{Name: "agent-darwin-arm64"}
	good := []byte(sum(bin) + "  agent-darwin-arm64\n")
	client, release := fakeReleaseServer(t, map[string][]byte{"agent-sha256sums.txt": good, "SHA256SUMS": []byte(strings.Repeat("0", 64) + "  agent-darwin-arm64\n"), "checksums.txt": []byte(sum(bin) + "  other\n")})
	ctx := context.Background()
	var errOut bytes.Buffer

	if err := verifyReleaseAsset(ctx, client, release("agent-sha256sums.txt"), "agent", asset, binPath, false, &errOut); err != nil {
		t.Errorf("good checksum: %v", err)
	}
	if err := verifyReleaseAsset(ctx, client, release(), "agent", asset, binPath, false, &errOut); err == nil || !strings.Contains(err.Error(), "--insecure-skip-checksum") {
		t.Errorf("no checksum asset: %v", err)
	}
	if err := verifyReleaseAsset(ctx, client, release("SHA256SUMS"), "agent", asset, binPath, false, &errOut); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("mismatch: %v", err)
	}
	if err := verifyReleaseAsset(ctx, client, release("checksums.txt"), "agent", asset, binPath, false, &errOut); err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("no entry: %v", err)
	}
	broken := &github.Release{TagName: "x", Assets: []github.Asset{{Name: "agent-sha256sums.txt", BrowserDownloadURL: "http://127.0.0.1:1/nope"}}}
	if err := verifyReleaseAsset(ctx, client, broken, "agent", asset, binPath, false, &errOut); err == nil {
		t.Error("download failure must fail")
	}
	errOut.Reset()
	if err := verifyReleaseAsset(ctx, client, release(), "agent", asset, binPath, true, &errOut); err != nil || !strings.Contains(errOut.String(), "WARNING") {
		t.Errorf("skip: err=%v warning=%q", err, errOut.String())
	}
}
