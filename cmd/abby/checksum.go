package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/teabranch/abbyfile/pkg/github"
)

// newGitHubClient is replaced in tests. No environment variable may redirect
// the API base URL: the client sends the user's GitHub token there.
var newGitHubClient = github.NewClient

// verifyReleaseAsset checks binPath against the release's checksum asset.
// Every failure is fatal unless skip is set, which prints a warning instead.
func verifyReleaseAsset(ctx context.Context, client *github.Client, release *github.Release, agent string, asset github.Asset, binPath string, skip bool, errOut io.Writer) error {
	if skip {
		fmt.Fprintf(errOut, "WARNING: --insecure-skip-checksum: installing %s from %s WITHOUT verifying its checksum\n", asset.Name, release.TagName)
		return nil
	}
	sumsAsset := findChecksumAsset(release, agent)
	if sumsAsset == nil {
		return fmt.Errorf("release %s has no checksum asset (looked for %s-sha256sums.txt, SHA256SUMS, checksums.txt); refusing to install — republish with `abby publish`, or pass --insecure-skip-checksum", release.TagName, agent)
	}
	f, err := os.CreateTemp("", "abbyfile-sums-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(f.Name())
	err = client.DownloadAsset(ctx, *sumsAsset, f)
	f.Close()
	if err != nil {
		return fmt.Errorf("downloading %s: %w; refusing to install (pass --insecure-skip-checksum to bypass)", sumsAsset.Name, err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return err
	}
	expected, ok := github.ParseChecksumFile(string(data))[asset.Name]
	if !ok {
		return fmt.Errorf("checksum file %s has no entry for %s; refusing to install (pass --insecure-skip-checksum to bypass)", sumsAsset.Name, asset.Name)
	}
	if err := github.VerifyChecksum(binPath, expected); err != nil {
		return fmt.Errorf("checksum verification failed for %s: %w", asset.Name, err)
	}
	return nil
}
