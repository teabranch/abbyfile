package github

import (
	"net/http"
	"strconv"
	"time"
)

// retryTransport wraps an http.RoundTripper with retry logic and
// exponential backoff. It respects Retry-After headers on 429/403.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
}

// NewRetryTransport returns an http.RoundTripper that retries on transient failures.
func NewRetryTransport(base http.RoundTripper, maxRetries int) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &retryTransport{base: base, maxRetries: maxRetries}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if resp != nil {
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
						backoff = time.Duration(secs) * time.Second
					}
				}
				resp.Body.Close()
			}
			time.Sleep(backoff)
		}

		resp, err = t.base.RoundTrip(req)
		if err != nil {
			continue
		}

		// Retry on rate limit (429) and some 5xx errors.
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout {
			continue
		}

		return resp, nil
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}
