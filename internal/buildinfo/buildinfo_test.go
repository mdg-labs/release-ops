package buildinfo

import (
	"os"
	"regexp"
	"testing"
)

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

func TestRenovateVersionIsTheOneTheWorkerImageIsBuiltOn(t *testing.T) {
	t.Parallel()

	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	m := regexp.MustCompile(`(?m)^ARG WORKER_RENOVATE_TAG=(\S+)$`).FindSubmatch(dockerfile)
	if m == nil {
		t.Fatal("the Dockerfile has no ARG WORKER_RENOVATE_TAG default")
	}
	if string(m[1]) != RenovateVersion {
		t.Errorf("RenovateVersion = %q, the Dockerfile builds the worker on %q", RenovateVersion, m[1])
	}
}

func TestRenovateSourceURLNamesThePinnedVersion(t *testing.T) {
	t.Parallel()

	if got, want := RenovateSourceURL(), "https://github.com/renovatebot/renovate/tree/"+RenovateVersion; got != want {
		t.Errorf("RenovateSourceURL() = %q, want %q", got, want)
	}
}
