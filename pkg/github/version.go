package github

import (
	"fmt"
	"strconv"
	"strings"
)

// CompareVersions compares two semantic versions (without "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareVersions(a, b string) (int, error) {
	aParts, err := parseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("parsing version %q: %w", a, err)
	}
	bParts, err := parseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("parsing version %q: %w", b, err)
	}

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1, nil
		}
		if aParts[i] > bParts[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(v string) ([3]int, error) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("expected 3 parts, got %d", len(parts))
	}

	var result [3]int
	for i, p := range parts {
		// Strip pre-release suffix (e.g., "0-beta" → "0").
		if idx := strings.IndexByte(p, '-'); idx >= 0 {
			p = p[:idx]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, fmt.Errorf("invalid number %q: %w", parts[i], err)
		}
		result[i] = n
	}
	return result, nil
}
