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

// planEntries plans one Change per writer per entry (writer-major order,
// entry names sorted), without applying anything. All-or-nothing: if any
// PlanAdd fails, the error is returned immediately and no Change is applied.
// This is what keeps a planning failure (e.g. an unparsable existing config
// file, or a --config-method cli refusal) from leaving another writer's
// change applied, or — for install — a binary copied with no working
// config: callers plan before doing anything else irreversible.
func planEntries(opts installOptions, scope runtimecfg.Scope, entries map[string]runtimecfg.ServerEntry) ([]runtimecfg.Change, error) {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)

	var planned []runtimecfg.Change
	for _, w := range opts.Writers {
		for _, n := range names {
			c, err := w.PlanAdd(scope, n, entries[n])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", w.Runtime(), err)
			}
			planned = append(planned, c)
		}
	}
	return planned, nil
}

// commitPlanned applies (or, DryRun, previews) each already-planned change,
// in order, stopping at the first error; changes already applied stay
// applied and are returned.
func commitPlanned(opts installOptions, planned []runtimecfg.Change) ([]appliedChange, error) {
	var done []appliedChange
	for _, c := range planned {
		a, err := commit(opts, c)
		if err != nil {
			return done, err
		}
		done = append(done, a)
	}
	return done, nil
}

// applyEntries plans every (writer, entry) change up front — see
// planEntries — then applies (or, DryRun, previews) them in order, printing
// each preview. It stops at the first apply error; changes already applied
// stay applied and are returned.
func applyEntries(opts installOptions, scope runtimecfg.Scope, entries map[string]runtimecfg.ServerEntry) ([]appliedChange, error) {
	planned, err := planEntries(opts, scope, entries)
	if err != nil {
		return nil, err
	}
	return commitPlanned(opts, planned)
}

// removeEntries plans/applies removal of name from every writer. Every
// writer is attempted regardless of an earlier writer's failure; a combined
// error is returned if any writer failed to plan or apply its removal, so
// the caller (uninstall) can leave its registry entry in place for a retry
// instead of losing track of a partially-uninstalled agent.
func removeEntries(opts installOptions, scope runtimecfg.Scope, name string) ([]appliedChange, error) {
	var done []appliedChange
	var errs []string
	for _, w := range opts.Writers {
		c, err := w.PlanRemove(scope, name)
		if err != nil {
			fmt.Fprintf(opts.Err, "warning: %s: %v\n", w.Runtime(), err)
			errs = append(errs, fmt.Sprintf("%s: %v", w.Runtime(), err))
			continue
		}
		if c.Noop {
			continue
		}
		a, err := commit(opts, c)
		if err != nil {
			fmt.Fprintf(opts.Err, "warning: %v\n", err)
			errs = append(errs, err.Error())
			continue
		}
		done = append(done, a)
	}
	if len(errs) > 0 {
		return done, fmt.Errorf("%d runtime(s) failed to remove %q: %s", len(errs), name, strings.Join(errs, "; "))
	}
	return done, nil
}

func commit(opts installOptions, c runtimecfg.Change) (appliedChange, error) {
	verb := "update"
	switch {
	case c.Noop:
		verb = "unchanged"
	case c.Remove:
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
		method := string(c.Method)
		if c.Noop {
			method += " (unchanged)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Runtime, c.Scope, method, c.Target, c.Server, backup)
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
