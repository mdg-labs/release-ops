package buildinfo

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"", "dev"},
		{"  ", "dev"},
		{"v0.1.0", "v0.1.0"},
		{" nightly-abc1234\n", "nightly-abc1234"},
	}
	for _, tc := range tests {
		if got := normalize(tc.in); got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCurrentReadsVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "v1.2.3"
	if got := Current(); got != "v1.2.3" {
		t.Fatalf("Current() = %q, want v1.2.3", got)
	}
	Version = ""
	if got := Current(); got != "dev" {
		t.Fatalf("Current() with empty Version = %q, want dev", got)
	}
}
