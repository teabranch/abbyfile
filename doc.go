// Package abbyfile is a framework for packaging AI agent logic as executable
// Go CLI binaries. Each agent binary contains its own system prompt, tool
// references, and memory management — packaged as versioned software.
//
// The binary does NOT call the Claude API. Claude Code is the LLM runtime.
// The binary is a packaging format that exposes everything through CLI commands:
//
//	agent --custom-instructions    # print system prompt
//	agent --describe               # JSON manifest of tools, memory, version
//	agent run-tool <name>          # execute a tool
//	agent memory read|write|list   # memory operations
//	agent serve-mcp                # MCP-over-stdio server
//
// # Declarative Build
//
// Agents are defined declaratively in an Abbyfile (YAML) and .md files:
//
//	# Abbyfile
//	version: "1"
//	agents:
//	  my-agent:
//	    path: .claude/agents/my-agent.md
//	    version: 1.0.0
//
// Build with the abby CLI:
//
//	abby build    # → build/my-agent (standalone binary)
//	abby install my-agent  # → .abbyfile/bin/ + MCP config (auto-detected runtimes)
//
// # Runtime Library
//
// Generated binaries import the runtime packages directly:
//
//	import "github.com/teabranch/abbyfile/pkg/agent"
//	import "github.com/teabranch/abbyfile/pkg/builtins"
package abbyfile
