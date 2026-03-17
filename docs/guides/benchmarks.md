# MCP Token Cost Benchmarks

## Background

A widely-cited article by Jannik Reinhard argues that MCP servers are "context hogs" — the GitHub MCP server (93 tools) costs ~55,000 tokens, and an enterprise stack can exceed 150,000 tokens. CLI tools cost 0 tokens since models know them from training data. The article shows a 35x token reduction for CLI over MCP.

**Abbyfile agents are fundamentally different.** They expose 6-15 focused tools per agent, not 93 platform-wide tools. Each agent has a single domain with a small, purpose-built toolset. This page documents our methodology for measuring and proving this difference.

## What Abbyfile Measures Differently

The article measures platform-wrapper MCP servers: GitHub (93 tools), Jira, Confluence, etc. These are designed to expose an entire platform's API surface as MCP tools.

Abbyfile agents follow a different pattern:
- **Focused domain**: each agent does one thing well (Go development, code review, debugging)
- **Small toolset**: 6-15 tools per agent, matching the task at hand
- **System prompt**: a structured prompt tailored to the domain (~800-2,000 tokens live)
- **Multiple agents**: if you need more capabilities, add another focused agent

This is the "Unix philosophy" applied to MCP: small, composable agents rather than monolithic tool servers.

## Methodology

### Token Estimation

We provide three token counting methods, validated against each other:

1. **bytes/4 heuristic** (`BytesEstimator`): fast, dependency-free, consistent for relative comparisons. Underestimates Claude's actual token count by ~31%.

2. **BPE tokenizer** (`BPECounter`): uses tiktoken's `cl100k_base` encoding (GPT-4's tokenizer). Underestimates Claude's actual token count by ~67% — Claude's tokenizer produces significantly more tokens than GPT-4's for the same input.

3. **Anthropic count_tokens API** (`AnthropicClient`): calls Claude's actual tokenizer via the free `POST /v1/messages/count_tokens` endpoint. Zero inference cost. This is ground truth.

Live validation revealed that both offline estimators underestimate Claude's real token counts. The bytes/4 heuristic is actually the closer proxy (31% under vs 67% under for BPE). All results below include live-validated numbers where available.

### What We Measure

1. **Tool schema overhead**: each tool registered via MCP has a JSON schema (name, description, input parameters). We serialize tools as MCP JSON and count bytes/tokens.

2. **System prompt overhead**: the agent's system prompt delivered via `ServerOptions.Instructions` during the MCP handshake.

3. **Total context budget**: schema + prompt as a percentage of the 128K context window.

### Measurement Methods

- **Unit benchmarks** (`benchmarks/`): use in-memory MCP transport (`gomcp.NewInMemoryTransports()`) to capture exactly what the protocol sends, with no subprocess overhead.

- **Integration benchmarks** (`internal/integration/bench_test.go`): build real agent binaries, connect via `gomcp.CommandTransport`, and measure actual wire cost.

- **Synthetic scaling**: generate tool registries at varying sizes (6, 10, 15, 20, 30, 50, 93) with realistic schemas matching builtin tool complexity (~170 bytes/tool average).

## Results

Run `make bench-report` to generate current numbers.

### Scaling Curve

Tool schema tokens grow linearly with tool count (~333 bytes/tool for focused agents):

```
Focused-agent tools (Abbyfile-style, 2-3 params):
   6 tools:    477 tokens  (0.4% of 128K)
  10 tools:    836 tokens  (0.7% of 128K)
  15 tools:  1,254 tokens  (1.0% of 128K)  <- recommended max
  93 tools:  7,762 tokens  (6.1% of 128K)

Platform-wrapper tools (GitHub MCP-style, 5-10 params):
   6 tools:  1,999 tokens  (1.6% of 128K)
  10 tools:  3,252 tokens  (2.5% of 128K)
  93 tools: 30,328 tokens (23.7% of 128K)
```

Platform tools average ~1,300 bytes/tool (3.9x larger than focused tools).

### Tokenizer Comparison (Live-Validated)

Live validation against Claude's actual tokenizer via the count_tokens API revealed that **both offline estimators underestimate**:

```
Community agents (avg across 5 agents, tools + prompt):
  Estimator          Avg tokens    vs Live
  bytes/4 heuristic      1,497     underestimates by 31%
  cl100k_base (BPE)      1,184     underestimates by 67%
  Claude (live)          1,979     ground truth
```

Per-agent live measurements:

```
Agent                    Live    Heuristic    BPE    Live/Heur  Live/BPE
cli-developer           1,937       1,460  1,158        1.33      1.67
code-reviewer           1,884       1,412  1,112        1.33      1.69
debugger                2,010       1,533  1,210        1.31      1.66
golang-pro              2,117       1,668  1,306        1.27      1.62
performance-engineer    1,948       1,463  1,135        1.33      1.72
```

Key insight: GPT-4's cl100k_base tokenizer is a **poor proxy** for Claude's tokenizer. Claude produces ~67% more tokens for the same input. The bytes/4 heuristic (31% under) is paradoxically the better estimator.

### Multi-Turn Cost Projection

Tool definitions are resent with every Claude API request. Over a 20-turn conversation:

```
Configuration                          Per-turn     1t        5t       10t       20t       50t
GitHub MCP (93 tools, article)          55000T    55000T   275000T   550000T  1100000T  2750000T
Abbyfile (10 tools + prompt)            1681T     1681T     8405T    16810T    33620T    84050T

Savings (cumulative tokens avoided):
  Reduction                             53319T    53319T   266595T   533190T  1066380T  2665950T
  Ratio                                    33x       33x       33x       33x       33x       33x
```

Over 20 turns: GitHub MCP costs 1,100,000T cumulative. Abbyfile costs 33,620T. **33x reduction.**

### Claude Code Baseline Analysis

Claude Code itself consumes context before any MCP servers are loaded. These are manual estimates from session transcripts — not live-validated (we can't send Claude Code's internal tool schemas to count_tokens). The bytes/4 heuristic underestimates by ~31%, so actual baseline is likely higher.

```
Claude Code system prompt: ~3,500 tokens (estimated)
Claude Code built-in tools (23): ~5,700 tokens (estimated)
Total baseline: ~9,200 tokens (7.2% of 128K, estimated)
Likely actual (×1.31): ~12,000 tokens (9.4% of 128K)

Marginal cost of adding agents (live-validated):
  Single Abbyfile agent   ~1,937 tokens (1.5% of 128K, live)
  5 community agents       ~9,896 tokens (7.7% of 128K, live)
  GitHub MCP (93 tools)   ~55,000 tokens (43% of 128K, article)
```

A focused Abbyfile agent adds ~1,937 tokens (live) — 1.5% of the context window.

### Article Methodology Comparison

Using the calibrated estimator (bytes/4 × 1.31) for comparison. Note: BPE (cl100k_base) was previously used here but live validation showed it underestimates Claude's tokenizer by 67%, making it less accurate than the calibrated heuristic.

```
Article: GitHub MCP (93 tools) = ~55,000 tokens
Our platform-style (93 tools)  = ~25,004 tokens (BPE) / ~39,729 tokens (calibrated)
Our focused-style (10 tools)   = ~686 tokens (BPE) / ~1,095 tokens (calibrated)

Focused 10-tool agent vs article's 93-tool server: 50x smaller (calibrated)
```

The gap between our platform-style generation and the article's measurement (~55K) is because real GitHub MCP schemas have deeper nesting, more enums, and longer descriptions than our synthetic versions.

## The 50-Tool Anti-Pattern

50+ tools per agent IS an anti-pattern. The data shows why:

- At 50 tools, schema alone consumes ~7% of the context window
- Add a system prompt and you're approaching 10%
- Stack multiple such servers and you've consumed the entire context

If you need 50 tools, you need multiple agents. Each agent should have:
- A focused domain (one area of expertise)
- 6-15 tools maximum
- A system prompt tailored to its domain

## Design Principle

**One agent, one domain, 6-15 tools max.**

This keeps each agent under 5% of the context budget, leaving 95%+ for actual conversation and reasoning. Even loading 5 focused agents simultaneously stays under 25% — far below the 43% consumed by a single GitHub MCP server.

The multi-turn analysis makes this even more compelling: over a typical 20-turn coding session, the cumulative token cost difference between a focused agent and a platform MCP server is over 1 million tokens.

## Skills vs Sub-agents vs Abbyfile

The industry has converged on two dominant patterns for extending Claude Code: **Agent Skills** (Anthropic's progressive disclosure pattern, launched Dec 2025) and **Sub-agents** (separate context windows via the Agent tool). Abbyfile occupies a different point in the design space.

### Three-Way Cost Model (Live-Validated)

Each approach has a fundamentally different cost structure per API turn. Live validation via Claude's count_tokens API confirms the ranking and provides exact numbers:

- **Agent Skills**: loaded prompt tokens (text in context, no tool schemas). Progressive disclosure means only active skills pay full cost.
- **Sub-agents**: baseline + tools + prompt per invocation. Each call opens a separate context window and re-pays Claude Code's full overhead.
- **Abbyfile**: tools + prompt as marginal tokens on the existing context window. Same API call, no baseline re-payment.

```
Per-turn/per-call cost (cli-developer, 6 tools):
                    Estimated     Live (count_tokens)
  Agent Skills:     ~3,000T       816T
  Sub-agents:       ~10,600T      6,688T
  Abbyfile:        ~1,460T       1,937T
```

### Feature Comparison Matrix

```
Feature                         Skills          Sub-agents      Abbyfile
------------------------------  --------------  --------------  --------------
Context cost (live)             816T loaded     6,688T/call     1,937T marginal
Executable tools                no              yes (inherited) yes (MCP)
Persistent memory               no              no              yes
Versioning                      no              no              semver
Distribution                    folder copy     no              abby install
Context isolation               no              yes             no
Validation/testing              no              no              yes
```

### Measured Results (Live-Validated)

Using a real community agent (cli-developer, 6 tools) measured with Claude's actual tokenizer:

**Skills comparison**: A loaded skill costs **816T** (live) — much cheaper than our 3,000T estimate, which was based on Anthropic's blog post examples rather than our actual agent prompts. Abbyfile costs **1,937T** (live), or **2.4x** a loaded skill. The trade-off: for 2.4x the token cost, you get executable tools, persistent memory, semantic versioning, and one-command distribution.

**Sub-agent comparison**: Sub-agents cost **3.5x** Abbyfile per invocation (6,688T vs 1,937T, live). The gap comes from re-paying Claude Code's baseline tools on every call.

```
Sub-agent vs Abbyfile cumulative cost (live):
  Invocations         1          3          5         10         20
  Sub-agent       6,688T    20,064T    33,440T    66,880T   133,760T
  Abbyfile       1,937T     5,811T     9,685T    19,370T    38,740T
  Ratio             3.5x       3.5x       3.5x       3.5x       3.5x
```

### What the estimates got wrong

Live validation corrected several assumptions in our offline estimates:

1. **Skill cost was 3.7x overestimated** (3,000T estimated vs 816T live). Our estimate came from Anthropic's blog post examples of typical skill prompts. The actual community agents have shorter prompts.

2. **Sub-agent baseline was ~37% overestimated** (10,660T estimated vs 6,688T live). Our synthetic baseline tools generated descriptions sized to approximate token counts, but Claude's tokenizer handles them more efficiently than the bytes/4 heuristic predicted.

3. **Abbyfile cost was 33% underestimated** (1,460T estimated vs 1,937T live). The bytes/4 heuristic consistently underestimates Claude's tokenizer.

4. **The ranking holds**: Skill (816T) < Abbyfile (1,937T) < Sub-agent (6,688T). The ratios changed but the conclusion is the same — Abbyfile is the cheapest option that provides executable tools.

### Why Abbyfile Is the Sweet Spot

Agent Skills are the lightest option at 816T — markdown files with progressive disclosure, ideal for context-only instructions without tools or memory.

Sub-agents provide context isolation but cost 6,688T per invocation — 3.5x Abbyfile's cost due to re-paying Claude Code's baseline on every call.

Abbyfile sits in the middle at 1,937T: executable tools, persistent memory, and versioned distribution at 2.4x the cost of a pure skill. For repeated use over a session, the cumulative savings over sub-agents are substantial (38,740T vs 133,760T over 20 invocations).

These are not mutually exclusive. An Abbyfile agent can coexist with skills in the same project, and sub-agents can invoke Abbyfile agents' MCP tools.

### Sources

- Agent Skills: [Anthropic engineering blog](https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills)
- Sub-agents: [Claude Code docs](https://code.claude.com/docs/en/sub-agents)
- MCP token bloat: [MCP issue #1576](https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1576)
- Claude Code lazy-load: [claude-code #11364](https://github.com/anthropics/claude-code/issues/11364) (67.3K tokens for 7 servers)

## Live Validation

We validate all offline estimates against Claude's actual tokenizer using the free `POST /v1/messages/count_tokens` API endpoint. No inference cost — just exact token counts.

```bash
ANTHROPIC_API_KEY=sk-... go test -run TestLive -v ./benchmarks/
```

Tests skip gracefully without the key.

### Results

**Estimator accuracy** (averaged across 5 community agents):

| Estimator | Avg error | Direction |
|-----------|-----------|-----------|
| bytes/4 heuristic | 31% | underestimates |
| cl100k_base (BPE) | 67% | underestimates |

**Three-way cost validation** (cli-developer, 6 tools):

| Approach | Live tokens | Our estimate | Error |
|----------|------------|--------------|-------|
| Skill (prompt only) | 816 | 3,000 | 3.7x over |
| Abbyfile (tools + prompt) | 1,937 | 1,460 | 33% under |
| Sub-agent (baseline + agent) | 6,688 | 10,660 | 37% over |

**Ranking confirmed**: Abbyfile (1,937T) costs 3.5x less than sub-agents (6,688T) per invocation.

### What each test measures

- **TestLiveAbbyfileTokenCount**: exact token count for a community agent vs heuristic/BPE estimates
- **TestLiveSubAgentTokenCount**: full sub-agent invocation cost (baseline + agent tools + prompt)
- **TestLiveThreeWayValidation**: all three configurations measured, ranking verified
- **TestLiveCalibration**: all 5 community agents, average correction factors computed

### Manual Claude Code Reproduction

Claude Code doesn't expose per-request token counts (see [issue #6308](https://github.com/anthropics/claude-code/issues/6308)). To inspect MCP handshake overhead manually:

```bash
CLAUDE_DEBUG=1 claude
```

This shows MCP traffic including tool schemas. Compare the JSON payload sizes with our benchmark measurements.

## How to Reproduce

```bash
# Unit benchmarks
make bench

# Integration benchmarks (builds real binaries)
make bench-integration

# Human-readable comparison report
make bench-report

# All benchmarks
make bench-all
```

### Individual test commands

```bash
# Scaling curve
go test -run TestScalingCurve -v ./benchmarks/

# Community agent measurements
go test -run TestCommunityAgents -v ./benchmarks/

# Anti-pattern analysis
go test -run TestAntiPatternThreshold -v ./benchmarks/

# Full comparison with article
go test -run TestComparisonWithArticle -v ./benchmarks/

# Tokenizer comparison (bytes/4 vs BPE)
go test -run TestTokenizerComparison -v ./benchmarks/

# Multi-turn cost projection
go test -run TestMultiTurnProjection -v ./benchmarks/

# Claude Code baseline analysis
go test -run TestClaudeCodeBaseline -v ./benchmarks/

# Article methodology side-by-side
go test -run TestArticleMethodology -v ./benchmarks/

# Skills vs Abbyfile comparison
go test -run TestSkillsComparison -v ./benchmarks/

# Sub-agent cost model
go test -run TestSubAgentCostModel -v ./benchmarks/

# Three-way comparison (skills vs sub-agents vs abbyfile)
go test -run TestThreeWayComparison -v ./benchmarks/

# Live validation (requires ANTHROPIC_API_KEY)
ANTHROPIC_API_KEY=sk-... go test -run TestLive -v ./benchmarks/

# Real binary measurements (requires agent builds)
go test -tags integration -run TestMeasureAllAgents -v -timeout 120s ./internal/integration/
```
