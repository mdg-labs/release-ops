package handlers

import (
	"net/http"

	"github.com/mdg-labs/release-ops/internal/buildinfo"
)

// AboutHandlers serves GET /api/v1/about (specs.html section 7.8): static build facts, open
// to a viewer.
type AboutHandlers struct{}

type aboutResponse struct {
	Version  string        `json:"version"`
	Renovate aboutRenovate `json:"renovate"`
}

type aboutRenovate struct {
	Version   string `json:"version"`
	License   string `json:"license"`
	SourceURL string `json:"sourceUrl"`
}

// Get answers the Release Ops build and the pinned Renovate version with its licence and
// the place its source is published.
func (h *AboutHandlers) Get(w http.ResponseWriter, _ *http.Request) {
	writeRepositoryJSON(w, http.StatusOK, aboutResponse{
		Version: buildinfo.Current(),
		Renovate: aboutRenovate{
			Version:   buildinfo.RenovateVersion,
			License:   buildinfo.RenovateLicense,
			SourceURL: buildinfo.RenovateSourceURL(),
		},
	})
}
