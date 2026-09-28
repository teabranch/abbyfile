package tools

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

// DefaultMaxOutputBytes bounds captured subprocess output in memory when no
// CommandPolicy sets MaxOutputBytes. The context budget bounds what reaches
// the model; this bounds what the agent process holds.
const DefaultMaxOutputBytes int64 = 10 << 20

// LimitedBuffer keeps the first limit bytes written and discards the rest.
// Write always reports success so a subprocess is never blocked on a full
// pipe. Safe for concurrent writers (stdout and stderr sharing one buffer).
type LimitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

// NewLimitedBuffer returns a buffer capped at limit bytes; limit <= 0 means
// DefaultMaxOutputBytes.
func NewLimitedBuffer(limit int64) *LimitedBuffer {
	if limit <= 0 {
		limit = DefaultMaxOutputBytes
	}
	return &LimitedBuffer{limit: limit}
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	room := b.limit - int64(b.buf.Len())
	if room <= 0 {
		b.truncated = b.truncated || len(p) > 0
		return len(p), nil
	}
	if int64(len(p)) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

// Truncated reports whether any bytes were discarded.
func (b *LimitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// String returns the kept bytes, plus a truncation marker when bytes were dropped.
func (b *LimitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return b.buf.String()
	}
	return b.buf.String() + fmt.Sprintf("\n[output truncated at %d bytes]", b.limit)
}

type outputLimitKey struct{}

// WithOutputLimit returns ctx carrying the output byte cap for subprocess capture.
func WithOutputLimit(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, outputLimitKey{}, n)
}

// OutputLimit returns the cap carried by ctx, or DefaultMaxOutputBytes.
func OutputLimit(ctx context.Context) int64 {
	if n, ok := ctx.Value(outputLimitKey{}).(int64); ok && n > 0 {
		return n
	}
	return DefaultMaxOutputBytes
}
