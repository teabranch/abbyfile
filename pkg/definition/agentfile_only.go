package definition

import "fmt"

// CheckAgentFileOnly rejects an agent marked binary: false that declares
// something only a compiled binary can serve.
func CheckAgentFileOnly(def *AgentDef) error {
	if len(def.CustomTools) > 0 {
		return fmt.Errorf("agent %q has binary: false but declares custom_tools, which need a binary; remove one or the other", def.Name)
	}
	if def.Memory {
		return fmt.Errorf("agent %q has binary: false but sets memory, which needs a binary; remove one or the other", def.Name)
	}
	return nil
}
