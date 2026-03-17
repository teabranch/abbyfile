package benchmarks

import (
	"encoding/json"
	"fmt"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// TokenCounter counts tokens in a string.
type TokenCounter interface {
	Count(s string) int
	Name() string
}

// LiveCorrectionFactor is the average ratio of Claude's actual token count
// to the bytes/4 heuristic, derived from live calibration against the
// Anthropic count_tokens API across 5 community agents.
//
// The bytes/4 heuristic underestimates Claude's tokenizer by ~31%.
// Multiply heuristic estimates by this factor for calibrated results.
const LiveCorrectionFactor = 1.31

// BytesEstimator estimates tokens using the bytes/4 heuristic.
// Fast and dependency-free. Underestimates Claude's actual token count
// by ~31% (see LiveCorrectionFactor). Use CorrectedEstimator for
// calibrated results.
type BytesEstimator struct{}

func (BytesEstimator) Count(s string) int { return (len(s) + 3) / 4 }
func (BytesEstimator) Name() string       { return "bytes/4" }

// CorrectedEstimator applies the live-calibrated correction factor to
// the bytes/4 heuristic. This is the recommended offline estimator —
// it closely matches Claude's actual tokenizer without requiring an API key.
type CorrectedEstimator struct{}

func (CorrectedEstimator) Count(s string) int {
	raw := (len(s) + 3) / 4
	return int(float64(raw)*LiveCorrectionFactor + 0.5)
}
func (CorrectedEstimator) Name() string { return "bytes/4 * 1.31 (calibrated)" }

// BPECounter counts tokens using tiktoken's cl100k_base encoding.
// This is the BPE tokenizer used by GPT-4. Despite being a real tokenizer,
// it is a poor proxy for Claude's tokenizer — it underestimates Claude's
// token count by ~67%. The bytes/4 heuristic (31% under) is paradoxically
// more accurate. Kept for comparison purposes.
type BPECounter struct {
	enc *tiktoken.Tiktoken
}

var (
	bpeOnce    sync.Once
	bpeCounter *BPECounter
	bpeErr     error
)

// NewBPECounter returns a BPE token counter using cl100k_base encoding.
// The encoding data is downloaded on first use and cached. Returns an error
// if the encoding cannot be loaded (e.g., no network on first run).
func NewBPECounter() (*BPECounter, error) {
	bpeOnce.Do(func() {
		enc, err := tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			bpeErr = fmt.Errorf("initializing cl100k_base encoding: %w", err)
			return
		}
		bpeCounter = &BPECounter{enc: enc}
	})
	return bpeCounter, bpeErr
}

func (c *BPECounter) Count(s string) int {
	return len(c.enc.Encode(s, nil, nil))
}

func (c *BPECounter) Name() string { return "cl100k_base (BPE)" }

// TokenizerComparison holds side-by-side measurements from two counters.
type TokenizerComparison struct {
	Label      string  `json:"label"`
	ByteCount  int     `json:"byte_count"`
	Heuristic  int     `json:"heuristic_tokens"`
	BPE        int     `json:"bpe_tokens"`
	Ratio      float64 `json:"ratio"` // BPE / Heuristic
	BytesPerBP float64 `json:"bytes_per_bpe_token"`
}

// CompareTokenizers measures the same content with both counters.
func CompareTokenizers(label, content string, bpe *BPECounter) TokenizerComparison {
	heuristic := EstimateTokens(content)
	bpeCount := bpe.Count(content)
	ratio := 0.0
	if heuristic > 0 {
		ratio = float64(bpeCount) / float64(heuristic)
	}
	bytesPerBPE := 0.0
	if bpeCount > 0 {
		bytesPerBPE = float64(len(content)) / float64(bpeCount)
	}
	return TokenizerComparison{
		Label:      label,
		ByteCount:  len(content),
		Heuristic:  heuristic,
		BPE:        bpeCount,
		Ratio:      ratio,
		BytesPerBP: bytesPerBPE,
	}
}

// MeasureToolsWith measures tools using the given TokenCounter.
// If counter is nil, falls back to BytesEstimator.
func MeasureToolsWith(defs []*tools.Definition, counter TokenCounter) *RegistryMeasurement {
	if counter == nil {
		counter = BytesEstimator{}
	}

	m := &RegistryMeasurement{
		ToolCount: len(defs),
		PerTool:   make([]ToolMeasurement, 0, len(defs)),
	}

	for _, def := range defs {
		tool := mcpToolJSON{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		}
		data, _ := json.Marshal(tool)
		tokens := counter.Count(string(data))
		tm := ToolMeasurement{
			Name:         def.Name,
			SchemaBytes:  len(data),
			SchemaTokens: tokens,
		}
		m.PerTool = append(m.PerTool, tm)
		m.SchemaBytes += len(data)
		m.SchemaTokens += tokens
	}

	return m
}
