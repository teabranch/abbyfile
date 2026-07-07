package tools

import "testing"

func TestShape_UnderCapPassthrough(t *testing.T) {
	b := ContextBudget{MaxOutputLines: 10, MaxOutputBytes: 1000, OnOverflow: OverflowHeadTail, HeadLines: 3, TailLines: 2}
	res := b.Shape("t", "a\nb\nc", nil)
	if res.Shaped {
		t.Fatalf("expected not shaped, got Shaped=true")
	}
	if res.Output != "a\nb\nc" {
		t.Fatalf("output mutated: %q", res.Output)
	}
}

func TestShape_HeadTailElidesMiddle(t *testing.T) {
	// 20 lines, cap at 5, keep 2 head + 2 tail.
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, string(rune('A'+i%26))+"-line")
	}
	raw := ""
	for i, l := range lines {
		if i > 0 {
			raw += "\n"
		}
		raw += l
	}
	b := ContextBudget{MaxOutputLines: 5, MaxOutputBytes: 100000, OnOverflow: OverflowHeadTail, HeadLines: 2, TailLines: 2}
	res := b.Shape("t", raw, nil)
	if !res.Shaped {
		t.Fatal("expected shaped")
	}
	if res.OriginalLines != 20 {
		t.Fatalf("OriginalLines = %d, want 20", res.OriginalLines)
	}
	if !contains(res.Output, "elided") {
		t.Fatalf("expected elision marker, got: %q", res.Output)
	}
	if !contains(res.Output, lines[0]) || !contains(res.Output, lines[19]) {
		t.Fatalf("head/tail lines missing from output: %q", res.Output)
	}
}

func TestShape_ByteBackstopDefeatsHugeLines(t *testing.T) {
	// One giant line: under line cap but over byte cap.
	huge := ""
	for i := 0; i < 5000; i++ {
		huge += "x"
	}
	b := ContextBudget{MaxOutputLines: 100, MaxOutputBytes: 1000, OnOverflow: OverflowHeadTail, HeadLines: 1, TailLines: 1}
	res := b.Shape("t", huge, nil)
	if !res.Shaped {
		t.Fatal("expected shaped by byte backstop")
	}
	if int64(len(res.Output)) > 1000 {
		t.Fatalf("output %d bytes exceeds byte cap 1000", len(res.Output))
	}
}

func TestShape_Passthrough_ExplicitStrategy(t *testing.T) {
	b := ContextBudget{MaxOutputLines: 1, MaxOutputBytes: 1, OnOverflow: OverflowPassthrough}
	res := b.Shape("t", "a\nb\nc\nd", nil)
	if res.Shaped {
		t.Fatal("passthrough must never shape")
	}
	if res.Output != "a\nb\nc\nd" {
		t.Fatalf("passthrough mutated output: %q", res.Output)
	}
}

func TestEffectiveFor_PerToolMerge(t *testing.T) {
	b := ContextBudget{MaxOutputLines: 100, OnOverflow: OverflowHeadTail, HeadLines: 5, TailLines: 5,
		PerTool: map[string]ContextBudget{"run_command": {OnOverflow: OverflowSpill}}}
	eff := b.effectiveFor("run_command")
	if eff.OnOverflow != OverflowSpill {
		t.Fatalf("per-tool OnOverflow not applied: %v", eff.OnOverflow)
	}
	if eff.MaxOutputLines != 100 {
		t.Fatalf("base MaxOutputLines lost in merge: %d", eff.MaxOutputLines)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
