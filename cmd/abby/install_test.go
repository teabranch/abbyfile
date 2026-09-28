package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/config"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

// Fix round 1, item 1: install --dry-run --model must not write config.yaml.
func TestApplyModelOverrideDryRunWritesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	opts := installOptions{DryRun: true, Out: &out, Err: &out}
	if err := applyModelOverride(opts, "test-agent", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.Path("test-agent")); !os.IsNotExist(err) {
		t.Error("dry-run created config.yaml")
	}
	if !strings.Contains(out.String(), "would set model override: test-agent → gpt-5") {
		t.Errorf("missing dry-run message: %s", out.String())
	}
}

func TestApplyModelOverrideWritesField(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	opts := installOptions{Out: &out, Err: &out}
	if err := applyModelOverride(opts, "test-agent", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.Path("test-agent")); err != nil {
		t.Fatalf("expected config.yaml to be written: %v", err)
	}
	if !strings.Contains(out.String(), "Set model override: test-agent → gpt-5") {
		t.Errorf("missing confirmation message: %s", out.String())
	}
}

// Fix round 1, item 2: a planning failure (broken existing .gemini config)
// must leave no binary copied and no config written.
func TestRunLocalInstallPlanFailureLeavesNoBinaryCopiedOrConfigWritten(t *testing.T) {
	d := chdir(t)
	os.MkdirAll(filepath.Join(d, "build"), 0o755)
	os.WriteFile(filepath.Join(d, "build", "test-agent"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	// An invalid .gemini/settings.json makes gemini's PlanAdd fail.
	os.MkdirAll(filepath.Join(d, ".gemini"), 0o755)
	os.WriteFile(filepath.Join(d, ".gemini", "settings.json"), []byte("{not valid json"), 0o600)

	var out, errb bytes.Buffer
	opts := installOptions{Writers: fileWriters(runtimecfg.ClaudeCode, runtimecfg.Gemini), Out: &out, Err: &errb}
	if _, err := runLocalInstall("test-agent", opts); err == nil {
		t.Fatal("expected an error from the broken gemini config")
	}
	if _, err := os.Stat(filepath.Join(d, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("claude-code's config must not have been written when gemini's plan failed")
	}
	if _, err := os.Stat(filepath.Join(d, ".abbyfile", "bin", "test-agent")); !os.IsNotExist(err) {
		t.Error("binary must not have been copied when planning failed")
	}
}

// Fix round 1, item 6: installMany's dry-run summary line says "Would
// install", not "Installed".
func TestInstallManyDryRunWording(t *testing.T) {
	d := chdir(t)
	os.MkdirAll(filepath.Join(d, "build"), 0o755)
	os.WriteFile(filepath.Join(d, "build", "a"), []byte("#!/bin/sh\nexit 1\n"), 0o755)

	var out bytes.Buffer
	opts := installOptions{DryRun: true, Writers: fileWriters(runtimecfg.ClaudeCode), Out: &out, Err: &out}
	if _, err := installMany([]string{"a"}, opts, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Would install") || !strings.Contains(out.String(), "(dry run)") {
		t.Errorf("missing dry-run wording: %s", out.String())
	}
	if strings.Contains(out.String(), "Installed 1/1") {
		t.Errorf("dry-run must not say Installed: %s", out.String())
	}
}
