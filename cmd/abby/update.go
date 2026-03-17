package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/teabranch/abbyfile/pkg/github"
	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update [agent-name]",
		Short: "Update installed agents to the latest version",
		Long: `Checks GitHub Releases for newer versions of installed agents and
downloads updates. Only agents installed from a remote source can be updated.

If no agent name is given, checks all remote-installed agents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return runUpdate(name)
		},
	}
}

func runUpdate(name string) error {
	regPath, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	entries := reg.List()
	if len(entries) == 0 {
		fmt.Println("No agents installed.")
		return nil
	}

	client := github.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	updated := 0
	for _, entry := range entries {
		if name != "" && entry.Name != name {
			continue
		}

		if entry.Source == "local" {
			if name != "" {
				fmt.Printf("%s: installed from local build, skipping (use 'abbyfile build && abbyfile install %s' to update)\n", entry.Name, entry.Name)
			}
			continue
		}

		ref, err := github.ParseRef(entry.Source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not parse source %q: %v\n", entry.Name, entry.Source, err)
			continue
		}

		release, err := client.LatestRelease(ctx, ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: could not check for updates: %v\n", entry.Name, err)
			continue
		}

		latestVersion := github.VersionFromTag(release.TagName)
		cmp, err := github.CompareVersions(entry.Version, latestVersion)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: version comparison error: %v\n", entry.Name, err)
			continue
		}

		if cmp >= 0 {
			fmt.Printf("%s: already up to date (v%s)\n", entry.Name, entry.Version)
			continue
		}

		fmt.Printf("%s: %s → %s\n", entry.Name, entry.Version, latestVersion)

		// Re-install from the specific new version.
		global := entry.Scope == "global"
		ref.Version = latestVersion
		newRef := fmt.Sprintf("github.com/%s/%s/%s@%s", ref.Owner, ref.Repo, ref.Agent, latestVersion)
		writers := runtimecfg.Detect()
		if err := runRemoteInstall(newRef, global, writers); err != nil {
			fmt.Fprintf(os.Stderr, "%s: update failed: %v\n", entry.Name, err)
			continue
		}
		updated++
	}

	if name != "" {
		if _, ok := reg.Get(name); !ok {
			return fmt.Errorf("agent %q is not installed", name)
		}
	}

	if updated == 0 {
		fmt.Println("All agents are up to date.")
	}
	return nil
}
