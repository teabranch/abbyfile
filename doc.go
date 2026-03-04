// Package agentfile is a framework for packaging AI agent logic as executable
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
// Agents are defined declaratively in an Agentfile (YAML) and .md files:
//
//	# Agentfile
//	version: "1"
//	agents:
//	  my-agent:
//	    path: .claude/agents/my-agent.md
//	    version: 1.0.0
//
// Build with the agentfile CLI:
//
//	agentfile build    # → build/my-agent (standalone binary)
//	agentfile install my-agent  # → .agentfile/bin/ + .mcp.json
//
// # Runtime Library
//
// Generated binaries import the runtime packages directly:
//
//	import "github.com/teabranch/agentfile/pkg/agent"
//	import "github.com/teabranch/agentfile/pkg/builtins"
package agentfile
