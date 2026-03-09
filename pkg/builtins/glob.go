package builtins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teabranch/agentfile/pkg/tools"
)

// GlobFilesTool returns a tool definition for file pattern matching.
func GlobFilesTool() *tools.Definition {
	return tools.BuiltinTool(
		"glob_files",
		"Find files matching a glob pattern",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern to match files (e.g., '**/*.go', 'src/*.ts')",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Base directory to search in (default: current directory)",
				},
			},
			"required": []string{"pattern"},
		},
		handleGlobFiles,
	).WithAnnotations(&tools.Annotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Title:          "Glob Files",
	})
}

func handleGlobFiles(input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: pattern")
	}

	baseDir := "."
	if p, ok := input["path"].(string); ok && p != "" {
		baseDir = p
	}

	// For patterns without **, use filepath.Glob directly.
	if !strings.Contains(pattern, "**") {
		fullPattern := filepath.Join(baseDir, pattern)
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return "", fmt.Errorf("globbing: %w", err)
		}
		sort.Strings(matches)
		if len(matches) == 0 {
			return "No files matched.", nil
		}
		return strings.Join(matches, "\n"), nil
	}

	// For ** patterns, walk the directory tree and match the suffix.
	parts := strings.SplitN(pattern, "**", 2)
	prefix := parts[0]
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimPrefix(parts[1], "/")
		suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
	}

	searchDir := filepath.Join(baseDir, prefix)
	var matches []string
	err := filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if suffix == "" {
			matches = append(matches, path)
			return nil
		}
		// Match against the relative path from searchDir, not just the filename,
		// so patterns like **/internal/*.go work correctly.
		rel, relErr := filepath.Rel(searchDir, path)
		if relErr != nil {
			return nil
		}
		matched, _ := filepath.Match(suffix, rel)
		if !matched {
			// Fall back to matching just the filename for simple suffixes like *.go
			matched, _ = filepath.Match(suffix, filepath.Base(path))
		}
		if matched {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking directory: %w", err)
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return "No files matched.", nil
	}
	return strings.Join(matches, "\n"), nil
}
