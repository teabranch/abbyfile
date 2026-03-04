package main

import "os"

// resolveAgentfile checks the working directory for known manifest filenames
// and returns the first that exists. Agentfile takes precedence over
// agentfile.yaml for backward compatibility. If neither exists it falls back
// to "Agentfile" so that ParseAgentfile produces the familiar error.
func resolveAgentfile() string {
	candidates := []string{"Agentfile", "agentfile.yaml"}
	for _, name := range candidates {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	return "Agentfile"
}
