package benchmarks

import (
	"fmt"

	"github.com/teabranch/abbyfile/pkg/tools"
)

// toolTemplate defines realistic property patterns matching builtin tool complexity.
// Each template has 2-3 properties with descriptions, producing ~170 bytes/tool avg.
var toolTemplates = []struct {
	suffix     string
	properties map[string]any
	required   []string
}{
	{
		suffix: "reader",
		properties: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to read",
			},
		},
		required: []string{"path"},
	},
	{
		suffix: "writer",
		properties: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		required: []string{"path", "content"},
	},
	{
		suffix: "search",
		properties: map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files",
			},
		},
		required: []string{"pattern"},
	},
	{
		suffix: "executor",
		properties: map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds",
			},
		},
		required: []string{"command"},
	},
	{
		suffix: "editor",
		properties: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the file to edit",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact string to find and replace",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement string",
			},
		},
		required: []string{"path", "old_string", "new_string"},
	},
}

// GenerateTools creates n tools with realistic schemas matching builtin tool
// complexity (~170 bytes/tool average). Tools cycle through templates to
// maintain realistic variation in property counts and descriptions.
func GenerateTools(n int) []*tools.Definition {
	defs := make([]*tools.Definition, n)
	for i := range n {
		tmpl := toolTemplates[i%len(toolTemplates)]
		defs[i] = &tools.Definition{
			Name:        fmt.Sprintf("tool_%03d_%s", i, tmpl.suffix),
			Description: fmt.Sprintf("Synthetic benchmark tool %d for scaling measurement", i),
			InputSchema: map[string]any{
				"type":       "object",
				"properties": tmpl.properties,
				"required":   tmpl.required,
			},
		}
	}
	return defs
}

// ScalingPoint represents a single data point in the scaling curve.
type ScalingPoint struct {
	ToolCount    int     `json:"tool_count"`
	SchemaBytes  int     `json:"schema_bytes"`
	SchemaTokens int     `json:"schema_tokens"`
	BudgetPct    float64 `json:"budget_pct"`
}

// MeasureScalingCurve measures schema cost at the given tool counts.
func MeasureScalingCurve(counts []int) []ScalingPoint {
	points := make([]ScalingPoint, len(counts))
	for i, n := range counts {
		defs := GenerateTools(n)
		m := MeasureTools(defs)
		points[i] = ScalingPoint{
			ToolCount:    n,
			SchemaBytes:  m.SchemaBytes,
			SchemaTokens: m.SchemaTokens,
			BudgetPct:    float64(m.SchemaTokens) / float64(ContextWindow) * 100,
		}
	}
	return points
}

// DefaultScalingSizes returns the standard set of tool counts for benchmarks.
func DefaultScalingSizes() []int {
	return []int{6, 10, 15, 20, 30, 50, 93}
}
