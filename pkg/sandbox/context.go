package sandbox

import (
	"context"
	"os"
)

type ctxKey struct{}

// NewContext returns a copy of ctx carrying s.
func NewContext(ctx context.Context, s *Sandbox) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the sandbox carried by ctx. Without one it returns
// the secure default for the process working directory; if that cannot be
// determined, a sandbox with no roots, which denies every path.
func FromContext(ctx context.Context) *Sandbox {
	if s, ok := ctx.Value(ctxKey{}).(*Sandbox); ok && s != nil {
		return s
	}
	if cwd, err := os.Getwd(); err == nil {
		if s, err := New(Default(), cwd); err == nil {
			return s
		}
	}
	return &Sandbox{cfg: Default().Normalize()}
}
