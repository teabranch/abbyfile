package benchmarks

import (
	"fmt"
	"strings"

	"github.com/teabranch/abbyfile/pkg/tools"
)

// platformToolTemplates model the schema complexity of GitHub MCP server tools.
// Each tool has 5-10 parameters with detailed descriptions, nested types,
// and arrays — matching the real GitHub MCP server's tool definitions.
var platformToolTemplates = []struct {
	suffix      string
	description string
	properties  map[string]any
	required    []string
}{
	{
		suffix:      "create_issue",
		description: "Create a new issue in a GitHub repository. The issue will be created with the given title and body. You can optionally assign users, add labels, set a milestone, and specify the issue template. Returns the created issue including its number, URL, and full metadata.",
		properties: map[string]any{
			"owner":     map[string]any{"type": "string", "description": "The account owner of the repository. This is the GitHub username or organization name that owns the repository."},
			"repo":      map[string]any{"type": "string", "description": "The name of the repository without the .git extension. Case-sensitive."},
			"title":     map[string]any{"type": "string", "description": "The title of the issue. Should be concise but descriptive enough to understand the issue at a glance."},
			"body":      map[string]any{"type": "string", "description": "The contents of the issue in GitHub-flavored Markdown format. Supports all standard Markdown syntax including code blocks, tables, and task lists."},
			"assignees": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "An array of GitHub usernames to assign to this issue. Each assignee must have push access to the repository."},
			"labels":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "An array of label names to associate with this issue. Labels must already exist in the repository."},
			"milestone": map[string]any{"type": "integer", "description": "The number of the milestone to associate this issue with. Use the milestone number, not the title."},
		},
		required: []string{"owner", "repo", "title"},
	},
	{
		suffix:      "create_pull_request",
		description: "Create a new pull request in a GitHub repository. Opens a pull request from a head branch to a base branch with the specified title and body. The head branch must contain commits not present in the base branch. Returns the created pull request with its number, URL, merge status, and review information.",
		properties: map[string]any{
			"owner":                 map[string]any{"type": "string", "description": "The account owner of the repository. This is the GitHub username or organization name."},
			"repo":                  map[string]any{"type": "string", "description": "The name of the repository without the .git extension."},
			"title":                 map[string]any{"type": "string", "description": "The title of the pull request. Should summarize the changes being proposed."},
			"body":                  map[string]any{"type": "string", "description": "The contents of the pull request body in GitHub-flavored Markdown. Should describe what changes were made and why."},
			"head":                  map[string]any{"type": "string", "description": "The name of the branch where your changes are implemented. For cross-repository PRs, prefix with username:branch."},
			"base":                  map[string]any{"type": "string", "description": "The name of the branch you want the changes pulled into. This is usually the default branch (main or master)."},
			"draft":                 map[string]any{"type": "boolean", "description": "Indicates whether the pull request is a draft. Draft PRs cannot be merged until marked as ready for review."},
			"maintainer_can_modify": map[string]any{"type": "boolean", "description": "Indicates whether maintainers can modify the pull request. Allows repository maintainers to push commits to the head branch."},
		},
		required: []string{"owner", "repo", "title", "head", "base"},
	},
	{
		suffix:      "search_repositories",
		description: "Search for repositories on GitHub using the repository search API. Supports complex queries with qualifiers for language, stars, forks, size, creation date, and more. Results are sorted by best match by default. Returns repository metadata including full name, description, star count, language, and license information.",
		properties: map[string]any{
			"query":    map[string]any{"type": "string", "description": "The search query. Can include qualifiers like language:go, stars:>100, pushed:>2024-01-01. See GitHub search syntax documentation for full qualifier list."},
			"sort":     map[string]any{"type": "string", "enum": []string{"stars", "forks", "help-wanted-issues", "updated"}, "description": "The sort field. Can be stars, forks, help-wanted-issues, or updated. Default: best match."},
			"order":    map[string]any{"type": "string", "enum": []string{"asc", "desc"}, "description": "The sort order. Can be asc or desc. Default: desc."},
			"per_page": map[string]any{"type": "integer", "description": "Number of results per page. Maximum 100. Default: 30."},
			"page":     map[string]any{"type": "integer", "description": "Page number of the results to fetch. Default: 1."},
		},
		required: []string{"query"},
	},
	{
		suffix:      "get_file_contents",
		description: "Get the contents of a file or directory in a GitHub repository. For files, returns the content along with metadata such as SHA, size, and encoding. For directories, returns an array of content objects. Content is returned as base64-encoded data that must be decoded. Supports fetching content from any branch or commit by specifying a ref.",
		properties: map[string]any{
			"owner": map[string]any{"type": "string", "description": "The account owner of the repository."},
			"repo":  map[string]any{"type": "string", "description": "The name of the repository without the .git extension."},
			"path":  map[string]any{"type": "string", "description": "The file path relative to the repository root. Do not include a leading slash."},
			"ref":   map[string]any{"type": "string", "description": "The name of the commit, branch, or tag. Default: the repository's default branch."},
		},
		required: []string{"owner", "repo", "path"},
	},
	{
		suffix:      "list_commits",
		description: "List commits for a repository. Commits are listed in reverse chronological order by default. You can filter by SHA/branch, path, author, and date range. Returns commit metadata including SHA, message, author information, committer information, and tree SHA. Supports pagination for repositories with many commits.",
		properties: map[string]any{
			"owner":    map[string]any{"type": "string", "description": "The account owner of the repository."},
			"repo":     map[string]any{"type": "string", "description": "The name of the repository without the .git extension."},
			"sha":      map[string]any{"type": "string", "description": "SHA or branch to start listing commits from. Default: the repository's default branch."},
			"path":     map[string]any{"type": "string", "description": "Only commits containing this file path will be returned."},
			"author":   map[string]any{"type": "string", "description": "GitHub username or email address to filter by commit author."},
			"since":    map[string]any{"type": "string", "description": "Only show commits after this date. ISO 8601 format: YYYY-MM-DDTHH:MM:SSZ."},
			"until":    map[string]any{"type": "string", "description": "Only show commits before this date. ISO 8601 format: YYYY-MM-DDTHH:MM:SSZ."},
			"per_page": map[string]any{"type": "integer", "description": "Number of results per page. Maximum 100. Default: 30."},
			"page":     map[string]any{"type": "integer", "description": "Page number of the results to fetch. Default: 1."},
		},
		required: []string{"owner", "repo"},
	},
}

// GeneratePlatformTools creates n tools with GitHub MCP-style schema complexity.
// Each tool has 5-10 parameters with detailed descriptions, nested types, and
// arrays — matching the real schema complexity that produces the article's
// ~55,000 token overhead for 93 tools (~590 tokens/tool average).
func GeneratePlatformTools(n int) []*tools.Definition {
	defs := make([]*tools.Definition, n)
	for i := range n {
		tmpl := platformToolTemplates[i%len(platformToolTemplates)]
		defs[i] = &tools.Definition{
			Name:        fmt.Sprintf("github_%03d_%s", i, tmpl.suffix),
			Description: tmpl.description,
			InputSchema: map[string]any{
				"type":       "object",
				"properties": tmpl.properties,
				"required":   tmpl.required,
			},
		}
	}
	return defs
}

// ToolComplexityComparison holds side-by-side metrics for focused vs platform tools.
type ToolComplexityComparison struct {
	FocusedPerTool  float64 `json:"focused_bytes_per_tool"`
	PlatformPerTool float64 `json:"platform_bytes_per_tool"`
	ComplexityRatio float64 `json:"complexity_ratio"`
	FocusedTotal    int     `json:"focused_total_tokens"`
	PlatformTotal   int     `json:"platform_total_tokens"`
}

// CompareToolComplexity measures focused-agent tools vs platform-wrapper tools
// at the same tool count, showing the per-tool schema size difference.
func CompareToolComplexity(toolCount int, counter TokenCounter) *ToolComplexityComparison {
	if counter == nil {
		counter = BytesEstimator{}
	}

	focused := MeasureToolsWith(GenerateTools(toolCount), counter)
	platform := MeasureToolsWith(GeneratePlatformTools(toolCount), counter)

	focusedPer := float64(focused.SchemaBytes) / float64(toolCount)
	platformPer := float64(platform.SchemaBytes) / float64(toolCount)

	return &ToolComplexityComparison{
		FocusedPerTool:  focusedPer,
		PlatformPerTool: platformPer,
		ComplexityRatio: platformPer / focusedPer,
		FocusedTotal:    focused.SchemaTokens,
		PlatformTotal:   platform.SchemaTokens,
	}
}

// FormatArticleComparison outputs the full article-vs-abbyfile analysis.
func FormatArticleComparison(counter TokenCounter) string {
	if counter == nil {
		counter = BytesEstimator{}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Article methodology comparison (tokenizer: %s):\n\n", counter.Name()))

	// Measure focused tools at key sizes.
	b.WriteString("  Focused-agent tools (Abbyfile-style, 2-3 params):\n")
	for _, n := range []int{6, 10, 15, 93} {
		m := MeasureToolsWith(GenerateTools(n), counter)
		perTool := float64(m.SchemaBytes) / float64(n)
		pct := float64(m.SchemaTokens) / float64(ContextWindow) * 100
		fmt.Fprintf(&b, "    %2d tools: %5d tokens (%4.1f%% of 128K)  ~%.0f bytes/tool\n",
			n, m.SchemaTokens, pct, perTool)
	}

	// Measure platform-wrapper tools at key sizes.
	b.WriteString("\n  Platform-wrapper tools (GitHub MCP-style, 5-10 params):\n")
	for _, n := range []int{6, 10, 15, 93} {
		m := MeasureToolsWith(GeneratePlatformTools(n), counter)
		perTool := float64(m.SchemaBytes) / float64(n)
		pct := float64(m.SchemaTokens) / float64(ContextWindow) * 100
		fmt.Fprintf(&b, "    %2d tools: %5d tokens (%4.1f%% of 128K)  ~%.0f bytes/tool\n",
			n, m.SchemaTokens, pct, perTool)
	}

	// Per-tool complexity comparison.
	cmp := CompareToolComplexity(93, counter)
	b.WriteString("\n  Per-tool schema complexity at 93 tools:\n")
	fmt.Fprintf(&b, "    Focused:  ~%.0f bytes/tool -> %d total tokens\n", cmp.FocusedPerTool, cmp.FocusedTotal)
	fmt.Fprintf(&b, "    Platform: ~%.0f bytes/tool -> %d total tokens\n", cmp.PlatformPerTool, cmp.PlatformTotal)
	fmt.Fprintf(&b, "    Schema complexity ratio: %.1fx\n", cmp.ComplexityRatio)

	// Final comparison with article's numbers.
	b.WriteString("\n  vs Article's measured numbers:\n")
	fmt.Fprintf(&b, "    Article: GitHub MCP (93 tools) = ~%d tokens\n", ArticleReference.GitHubMCP93Tools)
	fmt.Fprintf(&b, "    Our platform-style (93 tools)  = ~%d tokens\n", cmp.PlatformTotal)
	fmt.Fprintf(&b, "    Our focused-style (10 tools)   = ~%d tokens\n",
		MeasureToolsWith(GenerateTools(10), counter).SchemaTokens)

	ratio := float64(ArticleReference.GitHubMCP93Tools) / float64(MeasureToolsWith(GenerateTools(10), counter).SchemaTokens)
	fmt.Fprintf(&b, "    Focused 10-tool agent vs article's 93-tool server: %.0fx smaller\n", ratio)

	return b.String()
}
