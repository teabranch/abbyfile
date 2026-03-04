// Package tools provides tool registration, discovery, and subprocess execution
// for agentfile agents.
package tools

import "fmt"

// Annotations provides MCP tool annotation hints per tool.
// These are hints only — clients should not make tool use decisions based on
// untrusted server annotations.
type Annotations struct {
	ReadOnlyHint    bool
	DestructiveHint *bool // nil = MCP default (true)
	IdempotentHint  bool
	OpenWorldHint   *bool // nil = MCP default (true)
	Title           string
}

// Definition describes a tool that an agent can invoke.
type Definition struct {
	Name        string
	Description string
	InputSchema any
	Annotations *Annotations
	Builtin     bool // true for built-in tools (memory, etc.) — not executed as subprocesses
	StdinInput  bool // when true, pipe full input as JSON to stdin
	// For CLI tools
	Command string   // the binary to run
	Args    []string // default arguments
	// For built-in tools
	Handler func(input map[string]any) (string, error)
}

// WithAnnotations sets annotation hints on the definition and returns it for chaining.
func (d *Definition) WithAnnotations(a *Annotations) *Definition {
	d.Annotations = a
	return d
}

// BoolPtr returns a pointer to b. Use for Annotations fields where nil means
// "MCP default" and an explicit value overrides it.
func BoolPtr(b bool) *bool { return &b }

// Registry holds registered tool definitions.
type Registry struct {
	tools map[string]*Definition
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]*Definition)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(def *Definition) error {
	if def.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tool %q already registered", def.Name)
	}
	r.tools[def.Name] = def
	return nil
}

// Get returns a tool by name, or nil if not found.
func (r *Registry) Get(name string) *Definition {
	return r.tools[name]
}

// All returns all registered tool definitions.
func (r *Registry) All() []*Definition {
	defs := make([]*Definition, 0, len(r.tools))
	for _, def := range r.tools {
		defs = append(defs, def)
	}
	return defs
}

// CLI creates a Definition for a CLI tool.
func CLI(name, command, description string) *Definition {
	return &Definition{
		Name:        name,
		Description: description,
		Command:     command,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"args": map[string]any{
					"type":        "string",
					"description": "Command-line arguments to pass to the tool",
				},
			},
		},
	}
}

// Builtin creates a Definition for a built-in tool with a handler function.
func BuiltinTool(name, description string, schema any, handler func(input map[string]any) (string, error)) *Definition {
	return &Definition{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Builtin:     true,
		Handler:     handler,
	}
}
