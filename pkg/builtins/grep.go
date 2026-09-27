package builtins

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/teabranch/abbyfile/pkg/sandbox"
	"github.com/teabranch/abbyfile/pkg/tools"
)

// GrepSearchTool returns a tool definition for regex content search.
func GrepSearchTool() *tools.Definition {
	return tools.BuiltinToolCtx(
		"grep_search",
		"Search file contents using a regular expression pattern",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regular expression pattern to search for",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "File or directory to search in (default: current directory)",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter files (e.g., '*.go', '*.ts')",
				},
			},
			"required": []string{"pattern"},
		},
		handleGrepSearch,
	).WithAnnotations(&tools.Annotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
		Title:          "Grep Search",
	})
}

func handleGrepSearch(ctx context.Context, input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("missing required parameter: pattern")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	sb := sandbox.FromContext(ctx)
	searchPath := "."
	if p, ok := input["path"].(string); ok && p != "" {
		searchPath = p
	}
	globFilter, _ := input["glob"].(string)

	root, err := sb.Resolve(searchPath, sandbox.Read)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", searchPath, err)
	}
	const maxResults = 100
	if !info.IsDir() {
		return searchFile(root, searchPath, re, maxResults)
	}

	var results []string
	skipped := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if globFilter != "" {
			if matched, _ := filepath.Match(globFilter, filepath.Base(path)); !matched {
				return nil
			}
		}
		if !allowedEntry(sb, path, d) {
			skipped++
			return nil
		}
		label := displayPath(searchPath, root, path)
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if re.MatchString(scanner.Text()) {
				results = append(results, fmt.Sprintf("%s:%d:%s", label, lineNum, scanner.Text()))
				if len(results) >= maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("searching: %w", err)
	}
	if len(results) == 0 {
		return withSkipNote("No matches found.", skipped), nil
	}
	return withSkipNote(strings.Join(results, "\n"), skipped), nil
}

// searchFile greps one file at path, labelling hits with label.
func searchFile(path, label string, re *regexp.Regexp, maxResults int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	var results []string
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if re.MatchString(scanner.Text()) {
			results = append(results, fmt.Sprintf("%s:%d:%s", label, lineNum, scanner.Text()))
			if len(results) >= maxResults {
				break
			}
		}
	}
	if len(results) == 0 {
		return "No matches found.", nil
	}
	return strings.Join(results, "\n"), nil
}
