package builtins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// GlobFilesTool returns a tool definition for file pattern matching.
func GlobFilesTool() *tools.Definition {
	return tools.BuiltinToolCtx(
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

func handleGlobFiles(ctx context.Context, input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: pattern")
	}
	// Refuse ".." uniformly, before any globbing or walking. Without this,
	// a pattern like "../secret.go" would reach filepath.Glob, whose
	// success or failure (an existence Lstat done before any confinement
	// check) would leak whether a guessed path outside the sandbox exists.
	if containsDotDotElement(pattern) {
		return "", fmt.Errorf(`pattern %q must not contain ".." (use the path argument to choose the base directory)`, pattern)
	}
	sb := sandbox.FromContext(ctx)
	baseDir := "."
	if p, ok := input["path"].(string); ok && p != "" {
		baseDir = p
	}
	resolvedBase, err := sb.Resolve(baseDir, sandbox.Read)
	if err != nil {
		return "", err
	}

	var matches []string
	skipped := 0

	if !strings.Contains(pattern, "**") {
		found, err := filepath.Glob(filepath.Join(resolvedBase, pattern))
		if err != nil {
			return "", fmt.Errorf("globbing: %w", err)
		}
		// Glob follows symlinked directory components, so every hit is
		// still checked here. A denied hit is dropped silently, not
		// counted: the pattern can no longer contain "..", so the only way
		// to reach here is a symlinked directory, and counting the miss
		// would turn the skip note into an oracle for whether a guessed
		// path outside the sandbox exists.
		for _, m := range found {
			if _, err := sb.Resolve(m, sandbox.Read); err != nil {
				continue
			}
			matches = append(matches, displayPath(baseDir, resolvedBase, m))
		}
	} else {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := strings.TrimPrefix(strings.TrimPrefix(parts[1], "/"), string(filepath.Separator))
		// Resolve the walk root before walking: a prefix like "../../"
		// must be refused, not walked.
		searchDir, err := sb.Resolve(filepath.Join(resolvedBase, prefix), sandbox.Read)
		if err != nil {
			return "", err
		}
		err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if _, ok := allowedEntry(sb, path, d); !ok {
				skipped++
				return nil
			}
			if suffix != "" {
				rel, relErr := filepath.Rel(searchDir, path)
				if relErr != nil {
					return nil
				}
				matched, _ := filepath.Match(suffix, rel)
				if !matched {
					matched, _ = filepath.Match(suffix, filepath.Base(path))
				}
				if !matched {
					return nil
				}
			}
			matches = append(matches, displayPath(baseDir, resolvedBase, path))
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walking directory: %w", err)
		}
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return withSkipNote("No files matched.", skipped), nil
	}
	return withSkipNote(strings.Join(matches, "\n"), skipped), nil
}

// containsDotDotElement reports whether pattern has a ".." path element,
// checking both '/' and the OS separator so a forward-slash pattern is
// still caught on platforms whose separator differs.
func containsDotDotElement(pattern string) bool {
	for _, part := range strings.FieldsFunc(pattern, func(r rune) bool {
		return r == '/' || r == filepath.Separator
	}) {
		if part == ".." {
			return true
		}
	}
	return false
}
