package tools_test

import (
	"strings"
	"testing"

	"github.com/teabranch/abbyfile/pkg/memory"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// TestSpillToRealMemoryStore uses the real memory.Manager, whose key
// validation rejects path separators. A fake set func hid that bug.
func TestSpillToRealMemoryStore(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	mgr := memory.NewManager(store)
	sink := tools.NewMemorySink("my-agent", mgr.Set)

	b := tools.DefaultContextBudget()
	b.OnOverflow = tools.OverflowSpill
	b.MaxOutputLines = 10
	raw := strings.Repeat("line of output\n", 50)

	res := b.Shape("run_command", raw, sink)
	if res.Strategy != tools.OverflowSpill || res.SpillURI == "" {
		t.Fatalf("spill degraded: strategy=%q uri=%q output tail=%q",
			res.Strategy, res.SpillURI, res.Output[max(0, len(res.Output)-80):])
	}
	const prefix = "memory://my-agent/"
	if !strings.HasPrefix(res.SpillURI, prefix+"spill-run_command-") {
		t.Fatalf("SpillURI = %q", res.SpillURI)
	}
	got, err := mgr.Get(strings.TrimPrefix(res.SpillURI, prefix))
	if err != nil {
		t.Fatalf("reading spilled key back: %v", err)
	}
	if got != raw {
		t.Fatalf("spilled value mismatch: %d bytes, want %d", len(got), len(raw))
	}
}

// TestSpillPreviewReservesRoomForPointer guards against I1: hard-truncating
// the assembled "preview + pointer" string to MaxOutputBytes can cut the
// pointer off entirely when the preview alone already fills the cap (a few
// huge lines, or here one very long line). The fix must reserve room for the
// pointer suffix so the fetchable URI always survives when the cap is large
// enough to hold it.
func TestSpillPreviewReservesRoomForPointer(t *testing.T) {
	store, err := memory.NewFileStoreAt(t.TempDir(), memory.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	mgr := memory.NewManager(store)
	sink := tools.NewMemorySink("my-agent", mgr.Set)

	b := tools.DefaultContextBudget()
	b.OnOverflow = tools.OverflowSpill
	b.MaxOutputBytes = 1000
	raw := strings.Repeat("x", 5000) // one huge line, well past the byte cap.

	res := b.Shape("run_command", raw, sink)
	if res.Strategy != tools.OverflowSpill || res.SpillURI == "" {
		t.Fatalf("spill degraded: strategy=%q uri=%q", res.Strategy, res.SpillURI)
	}
	if int64(len(res.Output)) > b.MaxOutputBytes {
		t.Fatalf("output %d bytes exceeds cap %d", len(res.Output), b.MaxOutputBytes)
	}
	if !strings.Contains(res.Output, res.SpillURI) {
		t.Fatalf("output must contain the spill pointer URI %q when cap > len(suffix); got: %q", res.SpillURI, res.Output)
	}
}
