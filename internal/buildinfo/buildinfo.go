// Package buildinfo carries values stamped into the binary at image build time.
package buildinfo

import "strings"

const devVersion = "dev"

// Version is set with -ldflags "-X" at image build: the release tag (v1.2.3) or
// nightly-<short sha>. A plain `go build` leaves it at "dev".
var Version = devVersion

// Current returns the running version, "dev" when none was stamped in.
func Current() string {
	return normalize(Version)
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return devVersion
	}
	return v
}
