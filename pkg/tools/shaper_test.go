package tools

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

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
	if !strings.Contains(res.Output, "elided") {
		t.Fatalf("expected elision marker, got: %q", res.Output)
	}
	if !strings.Contains(res.Output, lines[0]) || !strings.Contains(res.Output, lines[19]) {
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

// stubSpillSink is a minimal SpillSink for tests: it always succeeds and
// returns a deterministic memory:// URI derived from the key.
type stubSpillSink struct{}

func (stubSpillSink) Put(key, value string) (string, error) {
	return "memory://test/" + key, nil
}

func TestShape_SpillFinalOutputRespectsByteCap(t *testing.T) {
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, "line")
	}
	raw := strings.Join(lines, "\n")

	b := ContextBudget{
		MaxOutputLines: 5,
		MaxOutputBytes: 120, // comfortably above the ~85-byte pointer suffix.
		OnOverflow:     OverflowSpill,
		HeadLines:      2,
		TailLines:      2,
	}
	res := b.Shape("t", raw, stubSpillSink{})
	if !res.Shaped {
		t.Fatal("expected shaped")
	}
	if int64(len(res.Output)) > b.MaxOutputBytes {
		t.Fatalf("final Output %d bytes exceeds MaxOutputBytes %d: %q", len(res.Output), b.MaxOutputBytes, res.Output)
	}
}

// TestShape_TinyCapStillIncludesFullPointer covers the I1 edge case: when
// MaxOutputBytes is smaller than the pointer suffix itself, the shaper still
// returns the full pointer rather than a truncated, useless URI fragment —
// deliberately exceeding the byte cap in this rare case, since a usable
// pointer beats a strictly-capped but broken one.
func TestShape_TinyCapStillIncludesFullPointer(t *testing.T) {
	b := ContextBudget{
		MaxOutputBytes: 5, // far smaller than any real pointer suffix.
		OnOverflow:     OverflowSpill,
		HeadLines:      2,
		TailLines:      2,
	}
	raw := strings.Repeat("line\n", 50)
	res := b.Shape("t", raw, stubSpillSink{})
	if !res.Shaped || res.SpillURI == "" {
		t.Fatalf("expected spill: shaped=%v uri=%q", res.Shaped, res.SpillURI)
	}
	want := fmt.Sprintf("\n\nFull output saved to %s. Fetch it if you need the elided detail.", res.SpillURI)
	if res.Output != want {
		t.Fatalf("tiny cap must still return the full pointer\ngot:  %q\nwant: %q", res.Output, want)
	}
}

func TestShape_DegradeFinalOutputRespectsByteCap(t *testing.T) {
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, "line")
	}
	raw := strings.Join(lines, "\n")

	b := ContextBudget{
		MaxOutputLines: 5,
		MaxOutputBytes: 120, // comfortably above the 41-byte degrade note.
		OnOverflow:     OverflowSpill,
		HeadLines:      2,
		TailLines:      2,
	}
	res := b.Shape("t", raw, nil)
	if !res.Shaped {
		t.Fatal("expected shaped")
	}
	if int64(len(res.Output)) > b.MaxOutputBytes {
		t.Fatalf("final Output %d bytes exceeds MaxOutputBytes %d: %q", len(res.Output), b.MaxOutputBytes, res.Output)
	}
}

// TestShape_TinyCapStillIncludesFullDegradeNote covers the I1 edge case for
// the degrade path (no sink / spill failed): when MaxOutputBytes is smaller
// than the degrade note itself, the shaper still returns the full note
// rather than a silently truncated fragment.
func TestShape_TinyCapStillIncludesFullDegradeNote(t *testing.T) {
	b := ContextBudget{
		MaxOutputBytes: 5, // far smaller than the degrade note.
		OnOverflow:     OverflowSpill,
		HeadLines:      2,
		TailLines:      2,
	}
	raw := strings.Repeat("line\n", 50)
	res := b.Shape("t", raw, nil) // nil sink forces the degrade path.
	want := "\n(spill unavailable — output truncated)"
	if res.Output != want {
		t.Fatalf("tiny cap must still return the full degrade note\ngot:  %q\nwant: %q", res.Output, want)
	}
}

// TestShape_DegradeNoteSurvivesByteCap guards against I1 for the degrade path
// (no sink / spill failed): the "(spill unavailable — output truncated)" note
// must survive the byte-cap backstop rather than being cut off because the
// preview alone already filled the cap.
func TestShape_DegradeNoteSurvivesByteCap(t *testing.T) {
	b := ContextBudget{
		MaxOutputBytes: 1000,
		OnOverflow:     OverflowSpill,
		HeadLines:      100,
		TailLines:      40,
	}
	raw := strings.Repeat("x", 5000) // one huge line, well past the byte cap.

	res := b.Shape("t", raw, nil) // nil sink forces the degrade path.
	if !res.Shaped {
		t.Fatal("expected shaped")
	}
	if int64(len(res.Output)) > b.MaxOutputBytes {
		t.Fatalf("output %d bytes exceeds cap %d", len(res.Output), b.MaxOutputBytes)
	}
	if !strings.Contains(res.Output, "spill unavailable") {
		t.Fatalf("output must contain the degrade note when cap > len(suffix); got: %q", res.Output)
	}
}

func TestShape_TruncationProducesValidUTF8(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "café—日本語")
	}
	raw := strings.Join(lines, "\n")

	for _, maxBytes := range []int64{20, 23, 24, 40, 55, 56} {
		b := ContextBudget{
			MaxOutputLines: 5,
			MaxOutputBytes: maxBytes,
			OnOverflow:     OverflowHeadTail,
			HeadLines:      2,
			TailLines:      2,
		}
		res := b.Shape("t", raw, nil)
		if !utf8.ValidString(res.Output) {
			t.Fatalf("MaxOutputBytes=%d: output is not valid UTF-8: %q", maxBytes, res.Output)
		}
	}
}
