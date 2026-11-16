// Package buildinfo carries values stamped into the binary at image build time.
package buildinfo

import "strings"

const devVersion = "dev"

// Version is set with -ldflags "-X" at image build: the release tag (v1.2.3) or
// nightly-<short sha>. A plain `go build` leaves it at "dev".
var Version = devVersion

// RenovateVersion is the Renovate release the worker image is built on and runs updates with.
// It is the WORKER_RENOVATE_TAG default of the Dockerfile; a test keeps the two equal.
const RenovateVersion = "44.239.1"

// RenovateLicense is the SPDX identifier of Renovate's licence.
const RenovateLicense = "AGPL-3.0-only"

// RenovateSourceURL is where the source of the pinned Renovate version is published.
func RenovateSourceURL() string {
	return "https://github.com/renovatebot/renovate/tree/" + RenovateVersion
}

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
