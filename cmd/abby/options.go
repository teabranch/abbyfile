package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/teabranch/abbyfile/pkg/registry"
	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

// installOptions carries per-invocation settings through install/build/uninstall/update.
type installOptions struct {
	Global       bool
	DryRun       bool
	SkipChecksum bool // Task 7
	// BinDir overrides the binary install directory; "" means
	// installBinDir(Global). update sets this to the entry's existing
	// directory so a re-install replaces the binary in place.
	BinDir string
	// ProjectRoot overrides the working directory used for a project-scope
	// entry's Cwd field; "" means os.Getwd(). uninstall/update/doctor act on
	// registry entries and may run from anywhere, so they set this to
	// projectRootFor(entry) instead of relying on the process's cwd.
	ProjectRoot string
	Writers     []runtimecfg.ConfigWriter
	Env         map[string]string
	Out, Err    io.Writer
}

// projectRootFor returns the project directory of a local install, whose
// binary lives at <root>/.abbyfile/bin/<name>; "" for global installs or
// unrecognised layouts (callers then fall back to the current directory).
func projectRootFor(e registry.Entry) string {
	if e.Scope == "global" {
		return ""
	}
	bin := filepath.Dir(e.Path)
	if filepath.Base(bin) != "bin" || filepath.Base(filepath.Dir(bin)) != ".abbyfile" {
		return ""
	}
	return filepath.Dir(filepath.Dir(bin))
}

func scopeFor(global bool) runtimecfg.Scope {
	if global {
		return runtimecfg.ScopeUser
	}
	return runtimecfg.ScopeProject
}

// cwdIfProject is the Cwd to set on a project-scope ServerEntry: opts.ProjectRoot
// when given (uninstall/update run from anywhere), else the process's own
// working directory (install/build, run from the project itself). User-scope
// entries never get a Cwd.
func cwdIfProject(scope runtimecfg.Scope, opts installOptions) string {
	if scope != runtimecfg.ScopeProject {
		return ""
	}
	if opts.ProjectRoot != "" {
		return opts.ProjectRoot
	}
	wd, _ := os.Getwd()
	return wd
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseEnvFlags parses repeated --env KEY=VALUE flags.
func parseEnvFlags(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || !envKey.MatchString(k) {
			return nil, fmt.Errorf("--env %q: want KEY=VALUE with KEY matching [A-Za-z_][A-Za-z0-9_]*", p)
		}
		env[k] = v
	}
	return env, nil
}

// configOptions resolves --config-method (flag, else ABBY_CONFIG_METHOD, else auto).
func configOptions(flag string) (runtimecfg.Options, error) {
	v := flag
	if v == "" {
		v = os.Getenv("ABBY_CONFIG_METHOD")
	}
	if v == "" {
		v = string(runtimecfg.MethodAuto)
	}
	m, err := runtimecfg.ParseMethod(v)
	if err != nil {
		return runtimecfg.Options{}, err
	}
	return runtimecfg.Options{Method: m}, nil
}
