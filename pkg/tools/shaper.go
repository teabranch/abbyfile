package tools

import (
	"fmt"
	"strings"
)

// OverflowStrategy selects how oversized tool output is shaped.
type OverflowStrategy string

const (
	OverflowHeadTail    OverflowStrategy = "head-tail"
	OverflowSpill       OverflowStrategy = "spill"
	OverflowPassthrough OverflowStrategy = "passthrough"
)

// ContextBudget caps how much tool output enters the context window.
type ContextBudget struct {
	MaxOutputLines    int
	MaxOutputBytes    int64
	OnOverflow        OverflowStrategy
	HeadLines         int
	TailLines         int
	SummaryLines      int
	EagerInstructions bool
	PerTool           map[string]ContextBudget
}

// SpillSink persists overflow output and returns a fetchable URI.
type SpillSink interface {
	Put(key, value string) (uri string, err error)
}

// ShapeResult is the outcome of shaping one tool's output.
type ShapeResult struct {
	Output        string
	Shaped        bool
	Strategy      OverflowStrategy
	OriginalLines int
	OriginalBytes int64
	SpillURI      string
}

// DefaultContextBudget returns the shipped defaults (protection on).
func DefaultContextBudget() ContextBudget {
	return ContextBudget{
		MaxOutputLines:    2000,
		MaxOutputBytes:    262144,
		OnOverflow:        OverflowHeadTail,
		HeadLines:         100,
		TailLines:         40,
		SummaryLines:      25,
		EagerInstructions: false,
	}
}

// effectiveFor merges a per-tool override (if any) over the base budget.
func (b ContextBudget) effectiveFor(toolName string) ContextBudget {
	pt, ok := b.PerTool[toolName]
	if !ok {
		return b
	}
	eff := b
	if pt.MaxOutputLines != 0 {
		eff.MaxOutputLines = pt.MaxOutputLines
	}
	if pt.MaxOutputBytes != 0 {
		eff.MaxOutputBytes = pt.MaxOutputBytes
	}
	if pt.OnOverflow != "" {
		eff.OnOverflow = pt.OnOverflow
	}
	if pt.HeadLines != 0 {
		eff.HeadLines = pt.HeadLines
	}
	if pt.TailLines != 0 {
		eff.TailLines = pt.TailLines
	}
	eff.PerTool = nil
	return eff
}

// Shape applies the budget to raw tool output.
func (b ContextBudget) Shape(toolName, raw string, sink SpillSink) ShapeResult {
	eff := b.effectiveFor(toolName)
	lines := strings.Split(raw, "\n")
	res := ShapeResult{
		Output:        raw,
		Strategy:      eff.OnOverflow,
		OriginalLines: len(lines),
		OriginalBytes: int64(len(raw)),
	}

	overLines := eff.MaxOutputLines > 0 && len(lines) > eff.MaxOutputLines
	overBytes := eff.MaxOutputBytes > 0 && int64(len(raw)) > eff.MaxOutputBytes
	if !overLines && !overBytes {
		return res // passthrough: under both caps.
	}
	if eff.OnOverflow == OverflowPassthrough {
		return res // explicit opt-out.
	}

	head := eff.HeadLines
	tail := eff.TailLines
	if head+tail == 0 {
		head = 1 // never produce an empty preview.
	}
	if head+tail >= len(lines) {
		head, tail = len(lines), 0
	}
	elidedLines := len(lines) - head - tail
	preview := b.assemble(lines, head, tail, res.OriginalLines, res.OriginalBytes, elidedLines)

	// Byte backstop: hard-truncate the assembled preview if still too big.
	if eff.MaxOutputBytes > 0 && int64(len(preview)) > eff.MaxOutputBytes {
		cut := int(eff.MaxOutputBytes)
		if cut > len(preview) {
			cut = len(preview)
		}
		preview = preview[:cut]
	}

	switch eff.OnOverflow {
	case OverflowSpill:
		if sink != nil {
			key := fmt.Sprintf("spill/%s", toolName)
			if uri, err := sink.Put(key, raw); err == nil {
				res.SpillURI = uri
				res.Output = preview + fmt.Sprintf("\n\nFull output saved to %s. Fetch it if you need the elided detail.", uri)
				res.Shaped = true
				return res
			}
		}
		// Degrade to head-tail when no sink or spill failed.
		res.Output = preview + "\n(spill unavailable — output truncated)"
		res.Strategy = OverflowHeadTail
		res.Shaped = true
		return res
	default: // OverflowHeadTail
		res.Output = preview
		res.Strategy = OverflowHeadTail
		res.Shaped = true
		return res
	}
}

// assemble builds the head + marker + tail preview.
func (b ContextBudget) assemble(lines []string, head, tail, totalLines int, totalBytes int64, elided int) string {
	var sb strings.Builder
	for i := 0; i < head && i < len(lines); i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("[… %d lines / %d bytes elided — full output not returned …]\n", elided, totalBytes))
	for i := len(lines) - tail; i < len(lines); i++ {
		if i < 0 {
			continue
		}
		sb.WriteString(lines[i])
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
