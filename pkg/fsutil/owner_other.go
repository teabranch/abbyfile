//go:build !unix

package fsutil

import "os"

// restoreOwnerFunc is a no-op where file ownership isn't a uid/gid pair.
func restoreOwnerFunc(string) func(*os.File) error { return nil }

// inheritOwnerFunc is a no-op where file ownership isn't a uid/gid pair.
func inheritOwnerFunc(string, []string) (func(*os.File) error, error) { return nil, nil }
