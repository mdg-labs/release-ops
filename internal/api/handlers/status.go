package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/buildinfo"
)

// StatusHandlers serves operational status HTTP endpoints.
type StatusHandlers struct{}

type statusResponse struct {
	Version string `json:"version"`
}

// Get handles GET /api/v1/status.
func (h *StatusHandlers) Get(w http.ResponseWriter, _ *http.Request) {
	writeStatusJSON(w, http.StatusOK, statusResponse{Version: buildinfo.Current()})
}

func writeStatusJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
