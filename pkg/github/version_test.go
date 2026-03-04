package github

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.0.1", "0.0.2", -1},
		{"1.2.3", "1.2.3", 0},
		{"2.0.0", "1.99.99", 1},
	}

	for _, tt := range tests {
		got, err := CompareVersions(tt.a, tt.b)
		if err != nil {
			t.Errorf("CompareVersions(%q, %q): %v", tt.a, tt.b, err)
			continue
		}
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCompareVersionsWithPrefix(t *testing.T) {
	got, err := CompareVersions("v1.0.0", "v0.9.0")
	if err != nil {
		t.Fatalf("CompareVersions: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestCompareVersionsInvalid(t *testing.T) {
	if _, err := CompareVersions("bad", "1.0.0"); err == nil {
		t.Error("expected error for invalid version")
	}
	if _, err := CompareVersions("1.0", "1.0.0"); err == nil {
		t.Error("expected error for 2-part version")
	}
}
