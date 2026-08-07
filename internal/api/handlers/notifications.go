package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

var validNotificationEvents = map[string]struct{}{
	"create":    {},
	"error":     {},
	"supersede": {},
}

// NotificationTester sends a probe message via Shoutrrr.
type NotificationTester interface {
	SendTest(ctx context.Context, shoutrrrURL string) error
}

// NotificationHandlers serves notification target HTTP endpoints.
type NotificationHandlers struct {
	Notifications store.NotificationTargetRepository
	Tester        NotificationTester
}

type notificationTargetResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	HasSecret bool     `json:"hasSecret"`
	Events    []string `json:"events"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

type createNotificationTargetRequest struct {
	Name        string   `json:"name"`
	ShoutrrrURL string   `json:"shoutrrrUrl"`
	Events      []string `json:"events"`
	Enabled     *bool    `json:"enabled"`
}

type patchNotificationTargetRequest struct {
	Name        string   `json:"name"`
	ShoutrrrURL *string  `json:"shoutrrrUrl"`
	Events      []string `json:"events"`
	Enabled     bool     `json:"enabled"`
}

type testNotificationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// List handles GET /api/v1/notification-targets.
func (h *NotificationHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.Notifications.List(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list notification targets", http.StatusInternalServerError)
		return
	}

	resp := make([]notificationTargetResponse, len(items))
	for i := range items {
		resp[i] = notificationTargetFromStore(&items[i])
	}

	writeNotificationsJSON(w, http.StatusOK, resp)
}

// Create handles POST /api/v1/notification-targets.
func (h *NotificationHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createNotificationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "name is required", http.StatusBadRequest)
		return
	}
	if req.ShoutrrrURL == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "shoutrrrUrl is required", http.StatusBadRequest)
		return
	}
	if err := validateNotificationEvents(req.Events); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	created, err := h.Notifications.Create(r.Context(), store.CreateNotificationTargetInput{
		Name:        req.Name,
		ShoutrrrURL: req.ShoutrrrURL,
		Events:      req.Events,
		Enabled:     enabled,
	})
	if err != nil {
		if errors.Is(err, store.ErrInvalidJSON) {
			auth.WriteError(w, "VALIDATION_ERROR", "events must be a non-empty JSON array", http.StatusBadRequest)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to create notification target", http.StatusInternalServerError)
		return
	}

	writeNotificationsJSON(w, http.StatusCreated, notificationTargetFromStore(created))
}

// Patch handles PATCH /api/v1/notification-targets/{id}.
func (h *NotificationHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "notification target id is required", http.StatusBadRequest)
		return
	}

	var req patchNotificationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "name is required", http.StatusBadRequest)
		return
	}
	if err := validateNotificationEvents(req.Events); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	input := store.UpdateNotificationTargetInput{
		Name:    req.Name,
		Events:  req.Events,
		Enabled: req.Enabled,
	}
	if req.ShoutrrrURL != nil {
		if *req.ShoutrrrURL == "" {
			auth.WriteError(w, "VALIDATION_ERROR", "shoutrrrUrl cannot be empty", http.StatusBadRequest)
			return
		}
		url := *req.ShoutrrrURL
		input.ShoutrrrURL = &url
	}

	updated, err := h.Notifications.Update(r.Context(), id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "notification target not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, store.ErrInvalidJSON) {
			auth.WriteError(w, "VALIDATION_ERROR", "events must be a non-empty JSON array", http.StatusBadRequest)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update notification target", http.StatusInternalServerError)
		return
	}

	writeNotificationsJSON(w, http.StatusOK, notificationTargetFromStore(updated))
}

// Delete handles DELETE /api/v1/notification-targets/{id}.
func (h *NotificationHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "notification target id is required", http.StatusBadRequest)
		return
	}

	if _, err := h.Notifications.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "notification target not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load notification target", http.StatusInternalServerError)
		return
	}

	if err := h.Notifications.Delete(r.Context(), id); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to delete notification target", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Test handles POST /api/v1/notification-targets/{id}/test.
func (h *NotificationHandlers) Test(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "notification target id is required", http.StatusBadRequest)
		return
	}
	if h.Tester == nil {
		auth.WriteError(w, "INTERNAL_ERROR", "notification tester not configured", http.StatusInternalServerError)
		return
	}

	if _, err := h.Notifications.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "notification target not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load notification target", http.StatusInternalServerError)
		return
	}

	url, err := h.Notifications.DecryptURL(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to decrypt notification target URL", http.StatusInternalServerError)
		return
	}

	// Detach from request cancellation so Shoutrrr send completes if the client disconnects.
	sendCtx := context.WithoutCancel(r.Context())
	if err := h.Tester.SendTest(sendCtx, url); err != nil {
		writeNotificationsJSON(w, http.StatusOK, testNotificationResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	writeNotificationsJSON(w, http.StatusOK, testNotificationResponse{Success: true})
}

func notificationTargetFromStore(item *store.NotificationTarget) notificationTargetResponse {
	events := item.Events
	if events == nil {
		events = []string{}
	}
	return notificationTargetResponse{
		ID:        item.ID,
		Name:      item.Name,
		HasSecret: item.HasSecret,
		Events:    events,
		Enabled:   item.Enabled,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func validateNotificationEvents(events []string) error {
	if len(events) == 0 {
		return nil
	}
	for _, event := range events {
		if _, ok := validNotificationEvents[event]; !ok {
			return errors.New("events must only contain: create, error, supersede")
		}
	}
	return nil
}

func writeNotificationsJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
