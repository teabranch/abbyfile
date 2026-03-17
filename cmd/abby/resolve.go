package main

import "os"

// resolveAbbyfile checks the working directory for known manifest filenames
// and returns the first that exists. Abbyfile takes precedence over
// abbyfile.yaml for backward compatibility. If neither exists it falls back
// to "Abbyfile" so that ParseAbbyfile produces the familiar error.
func resolveAbbyfile() string {
	candidates := []string{"Abbyfile", "abbyfile.yaml"}
	for _, name := range candidates {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	return "Abbyfile"
}
