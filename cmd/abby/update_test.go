package main

import "testing"

// Fix round 1, item 5: an invalid ABBY_CONFIG_METHOD must fail runUpdate,
// the same way install/uninstall fail on a bad --config-method/env value,
// rather than being silently ignored.
func TestRunUpdateFailsOnBadConfigMethodEnv(t *testing.T) {
	t.Setenv("ABBY_CONFIG_METHOD", "bogus")
	t.Setenv("HOME", t.TempDir())
	if err := runUpdate(""); err == nil {
		t.Error("expected an error from an invalid ABBY_CONFIG_METHOD")
	}
}
