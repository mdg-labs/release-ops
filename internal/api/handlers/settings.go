package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/renovate/redact"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxSettingsPatchBytes bounds the body of a settings PATCH, which holds one number.
const maxSettingsPatchBytes = 4 << 10

// SettingsHandlers serves the instance settings and the global Renovate config view of
// specs.html section 7.2. Each handler checks the admin role itself as well as the router.
type SettingsHandlers struct {
	Settings store.InstanceSettingsRepository
	// Config returns the environment configuration shown read-only.
	Config func() store.InstanceConfig
	// PollingMode is true when no webhook is configured (WEBHOOK_URL unset).
	PollingMode bool
}

// globalConfigOrigin says where the global Renovate config comes from.
type globalConfigOrigin struct {
	Source     *string `json:"source"`
	Repository *string `json:"repository"`
	Ref        *string `json:"ref"`
}

type settingsResponse struct {
	ConcurrencyCap     int64              `json:"concurrencyCap"`
	Mode               string             `json:"mode"`
	PresetRepositories []string           `json:"presetRepositories"`
	GlobalConfig       globalConfigOrigin `json:"globalConfig"`
}

type globalConfigResponse struct {
	globalConfigOrigin
	CapturedAt *string `json:"capturedAt"`
	Content    *string `json:"content"`
}

// origin is where the worker reads the global config: the source of the last captured copy
// when there is one, otherwise the configured repository, otherwise unknown. The repository
// and ref are the configured ones and are named only for a repository source.
func origin(cfg store.InstanceConfig, captured store.CapturedGlobalConfig) globalConfigOrigin {
	source := ""
	switch {
	case captured.Captured && captured.Source != "":
		source = captured.Source
	case cfg.GlobalConfigRepo != "":
		source = "repository"
	}
	out := globalConfigOrigin{}
	if source == "" {
		return out
	}
	out.Source = &source
	if source == "repository" && cfg.GlobalConfigRepo != "" {
		repo := cfg.GlobalConfigRepo
		out.Repository = &repo
		out.Ref = emptyToNil(cfg.GlobalConfigRef)
	}
	return out
}

func (h *SettingsHandlers) response(r *http.Request) (settingsResponse, error) {
	concurrency, err := h.Settings.ConcurrencyCap(r.Context())
	if err != nil {
		return settingsResponse{}, err
	}
	captured, err := h.Settings.GlobalConfig(r.Context())
	if err != nil {
		return settingsResponse{}, err
	}
	cfg := h.Config()
	mode := "webhook"
	if h.PollingMode {
		mode = "polling"
	}
	presets := cfg.PresetRepositories
	if presets == nil {
		presets = []string{}
	}
	return settingsResponse{ConcurrencyCap: concurrency, Mode: mode, PresetRepositories: presets, GlobalConfig: origin(cfg, captured)}, nil
}

// Get handles GET /api/v1/settings.
func (h *SettingsHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	resp, err := h.response(r)
	if err != nil {
		slog.Error("settings: loading failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}
	writeRepositoryJSON(w, http.StatusOK, resp)
}

// Patch handles PATCH /api/v1/settings: { "concurrencyCap"? }, an integer of at least 1 (422
// VALIDATION_ERROR otherwise). It answers with the settings as they are afterwards.
func (h *SettingsHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingsPatchBytes)).Decode(&body); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	for key := range body {
		if key != "concurrencyCap" {
			auth.WriteError(w, "VALIDATION_ERROR", "unknown field "+key, http.StatusBadRequest)
			return
		}
	}

	if raw, present := body["concurrencyCap"]; present {
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil || n < 1 {
			auth.WriteError(w, "VALIDATION_ERROR", "concurrencyCap must be an integer of at least 1", http.StatusUnprocessableEntity)
			return
		}
		if err := h.Settings.SetConcurrencyCap(r.Context(), n); err != nil {
			if errors.Is(err, store.ErrInvalidConcurrencyCap) {
				auth.WriteError(w, "VALIDATION_ERROR", "concurrencyCap must be an integer of at least 1", http.StatusUnprocessableEntity)
				return
			}
			slog.Error("settings: update failed", "err", err)
			auth.WriteError(w, "INTERNAL_ERROR", "failed to update settings", http.StatusInternalServerError)
			return
		}
	}

	resp, err := h.response(r)
	if err != nil {
		slog.Error("settings: reading back failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load settings", http.StatusInternalServerError)
		return
	}
	writeRepositoryJSON(w, http.StatusOK, resp)
}

// GetGlobalConfig handles GET /api/v1/settings/global-config: the global Renovate config as
// the worker last used it, as raw text and never parsed. Token-shaped strings are replaced
// in the text and nothing else of it changes. content is null until a job has returned one.
func (h *SettingsHandlers) GetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	captured, err := h.Settings.GlobalConfig(r.Context())
	if err != nil {
		slog.Error("settings: loading the global config failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load the global config", http.StatusInternalServerError)
		return
	}
	resp := globalConfigResponse{globalConfigOrigin: origin(h.Config(), captured)}
	if captured.Captured {
		content := redact.Shapes(captured.Content)
		resp.Content = &content
		resp.CapturedAt = emptyToNil(captured.CapturedAt)
	}
	writeRepositoryJSON(w, http.StatusOK, resp)
}
