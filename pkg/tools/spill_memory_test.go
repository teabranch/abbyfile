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
