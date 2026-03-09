package github

import (
	"net/http"
	"testing"
)

func TestParseLinkHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "empty header",
			header: "",
			want:   "",
		},
		{
			name:   "next link",
			header: `<https://api.github.com/repos/owner/repo/releases?page=2>; rel="next", <https://api.github.com/repos/owner/repo/releases?page=5>; rel="last"`,
			want:   "https://api.github.com/repos/owner/repo/releases?page=2",
		},
		{
			name:   "only last",
			header: `<https://api.github.com/repos/owner/repo/releases?page=5>; rel="last"`,
			want:   "",
		},
		{
			name:   "next only",
			header: `<https://api.github.com/repos/owner/repo/releases?page=3>; rel="next"`,
			want:   "https://api.github.com/repos/owner/repo/releases?page=3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLinkHeader(tt.header)
			if got != tt.want {
				t.Errorf("parseLinkHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRetryTransport(t *testing.T) {
	rt := NewRetryTransport(nil, 3)
	if rt == nil {
		t.Fatal("expected non-nil transport")
	}
	// Verify it implements http.RoundTripper
	var _ http.RoundTripper = rt
}
