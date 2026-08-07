package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

const minPollIntervalMinutes int64 = 5

// SettingsHandlers serves settings HTTP endpoints.
type SettingsHandlers struct {
	Settings store.SettingsRepository
}

type settingsResponse struct {
	PollIntervalMinutes int64 `json:"pollIntervalMinutes"`
}

type patchSettingsRequest struct {
	PollIntervalMinutes int64 `json:"pollIntervalMinutes"`
}

// Get handles GET /api/v1/settings.
func (h *SettingsHandlers) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Settings.Get(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}
	writeSettingsJSON(w, http.StatusOK, settings)
}

// Patch handles PATCH /api/v1/settings.
func (h *SettingsHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	var req patchSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.PollIntervalMinutes < minPollIntervalMinutes {
		auth.WriteError(w, "VALIDATION_ERROR", "pollIntervalMinutes must be at least 5", http.StatusBadRequest)
		return
	}

	settings, err := h.Settings.UpdatePollInterval(r.Context(), req.PollIntervalMinutes)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update settings", http.StatusInternalServerError)
		return
	}
	writeSettingsJSON(w, http.StatusOK, settings)
}

func writeSettingsJSON(w http.ResponseWriter, status int, settings *store.AppSettings) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(settingsResponse{
		PollIntervalMinutes: settings.PollIntervalMinutes,
	})
}
