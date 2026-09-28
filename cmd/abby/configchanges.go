package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/teabranch/abbyfile/pkg/runtimecfg"
)

type appliedChange struct {
	Change runtimecfg.Change
	Backup string
}

// applyEntries plans (and unless DryRun, applies) one change per writer per
// entry, printing each preview. It stops at the first error; changes already
// applied stay applied and are returned.
func applyEntries(opts installOptions, scope runtimecfg.Scope, entries map[string]runtimecfg.ServerEntry) ([]appliedChange, error) {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(opts.Env) > 0 && scope == runtimecfg.ScopeProject {
		fmt.Fprintln(opts.Err, "warning: --env values are written into project config files, which are often committed; prefer ${VAR} references (Claude Code and Gemini CLI expand them) for secrets")
	}
	var done []appliedChange
	for _, w := range opts.Writers {
		for _, n := range names {
			c, err := w.PlanAdd(scope, n, entries[n])
			if err != nil {
				return done, fmt.Errorf("%s: %w", w.Runtime(), err)
			}
			if a, err := commit(opts, c); err != nil {
				return done, err
			} else {
				done = append(done, a)
			}
		}
	}
	return done, nil
}

// removeEntries plans/applies removal of name from every writer.
func removeEntries(opts installOptions, scope runtimecfg.Scope, name string) ([]appliedChange, error) {
	var done []appliedChange
	for _, w := range opts.Writers {
		c, err := w.PlanRemove(scope, name)
		if err != nil {
			fmt.Fprintf(opts.Err, "warning: %s: %v\n", w.Runtime(), err)
			continue
		}
		if c.Noop {
			continue
		}
		a, err := commit(opts, c)
		if err != nil {
			fmt.Fprintf(opts.Err, "warning: %v\n", err)
			continue
		}
		done = append(done, a)
	}
	return done, nil
}

func commit(opts installOptions, c runtimecfg.Change) (appliedChange, error) {
	verb := "update"
	if c.Remove {
		verb = "remove"
	}
	fmt.Fprintf(opts.Out, "%s %s in %s (%s, %s scope, via %s):\n%s", verb, c.Server, c.Target, c.Runtime, c.Scope, c.Method, indent(c.Preview))
	if opts.DryRun {
		return appliedChange{Change: c}, nil
	}
	// Apply, not a c.Noop short-circuit here: Change.Apply's own contract
	// (pkg/runtimecfg) re-checks a MethodFile change even when it planned as
	// Noop, so a config file that changed between plan and apply (e.g. the
	// entry was removed by another process) is still corrected; a MethodCLI
	// Noop is skipped by Apply itself.
	backup, err := c.Apply()
	if err != nil {
		return appliedChange{}, fmt.Errorf("%s (%s): %w", c.Target, c.Runtime, err)
	}
	return appliedChange{Change: c, Backup: backup}, nil
}

func indent(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    " + l + "\n")
	}
	return b.String()
}

// printSummary prints one table of what was (or, with dryRun, would be) changed.
func printSummary(w io.Writer, applied []appliedChange, dryRun bool) {
	if len(applied) == 0 {
		return
	}
	title := "Runtime config changes:"
	if dryRun {
		title = "Planned runtime config changes (dry run — nothing written):"
	}
	fmt.Fprintln(w, "\n"+title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUNTIME\tSCOPE\tMETHOD\tTARGET\tSERVER\tBACKUP")
	var notes []string
	seen := map[string]bool{}
	for _, a := range applied {
		c := a.Change
		backup := a.Backup
		if backup == "" {
			backup = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Runtime, c.Scope, c.Method, c.Target, c.Server, backup)
		for _, n := range c.Notes {
			if !seen[n] {
				seen[n] = true
				notes = append(notes, n)
			}
		}
	}
	tw.Flush()
	for _, n := range notes {
		fmt.Fprintln(w, "note: "+n)
	}
}
