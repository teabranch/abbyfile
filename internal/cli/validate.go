package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teabranch/agentfile/pkg/prompt"
	"github.com/teabranch/agentfile/pkg/tools"
)

// NewValidateCommand creates the `validate` subcommand that checks agent wiring.
func NewValidateCommand(name, version string, loader *prompt.Loader, registry *tools.Registry, memoryEnabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check that the agent is configured correctly",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			var failed bool

			// 1. Check prompt loads
			p, err := loader.Load()
			if err != nil {
				fmt.Fprintf(w, "[FAIL] Prompt: %v\n", err)
				failed = true
			} else {
				fmt.Fprintf(w, "[PASS] Prompt: loaded (%d bytes)\n", len(p))
			}

			// 2. Check tools
			allTools := registry.All()
			for _, def := range allTools {
				if def.Builtin {
					if def.Handler != nil {
						fmt.Fprintf(w, "[PASS] Tool %q: builtin handler registered\n", def.Name)
					} else {
						fmt.Fprintf(w, "[FAIL] Tool %q: builtin tool has no handler\n", def.Name)
						failed = true
					}
				} else {
					path, err := exec.LookPath(def.Command)
					if err != nil {
						fmt.Fprintf(w, "[FAIL] Tool %q: command %q not found in PATH\n", def.Name, def.Command)
						failed = true
					} else {
						fmt.Fprintf(w, "[PASS] Tool %q: command %q found at %s\n", def.Name, def.Command, path)
					}
				}
			}

			// 3. Check memory
			if memoryEnabled {
				home, err := os.UserHomeDir()
				if err != nil {
					fmt.Fprintf(w, "[FAIL] Memory: cannot determine home directory: %v\n", err)
					failed = true
				} else {
					memDir := filepath.Join(home, ".agentfile", name, "memory")
					if err := os.MkdirAll(memDir, 0o755); err != nil {
						fmt.Fprintf(w, "[FAIL] Memory: cannot create directory %s: %v\n", memDir, err)
						failed = true
					} else {
						// Test writability
						testFile := filepath.Join(memDir, ".validate-test")
						if err := os.WriteFile(testFile, []byte("ok"), 0o644); err != nil {
							fmt.Fprintf(w, "[FAIL] Memory: directory %s is not writable: %v\n", memDir, err)
							failed = true
						} else {
							os.Remove(testFile)
							fmt.Fprintf(w, "[PASS] Memory: directory %s is writable\n", memDir)
						}
					}
				}
			} else {
				fmt.Fprintf(w, "[INFO] Memory: disabled\n")
			}

			// 4. Override status
			if loader.IsOverridden() {
				fmt.Fprintf(w, "[INFO] Override: active at %s\n", loader.OverridePath())
			} else {
				fmt.Fprintf(w, "[INFO] Override: not active (using embedded prompt)\n")
			}

			// 5. Version
			if version == "" {
				fmt.Fprintf(w, "[FAIL] Version: not set\n")
				failed = true
			} else {
				fmt.Fprintf(w, "[PASS] Version: %s\n", version)
			}

			// Summary
			fmt.Fprintln(w, strings.Repeat("-", 40))
			if failed {
				fmt.Fprintln(w, "Validation FAILED — see [FAIL] items above")
				return fmt.Errorf("validation failed")
			}
			fmt.Fprintln(w, "Validation PASSED")
			return nil
		},
	}
}
