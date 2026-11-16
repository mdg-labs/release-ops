package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// UsersHandlers serves the read-only list of the people who have signed in.
type UsersHandlers struct {
	Queries *storedb.Queries
}

type userListItem struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	CreatedAt string `json:"createdAt"`
}

type listUsersResponse struct {
	Items []userListItem `json:"items"`
}

// List handles GET /api/v1/users: every user with a GitHub identity, by their current login.
func (h *UsersHandlers) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Queries.ListUsers(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list users", http.StatusInternalServerError)
		return
	}

	items := make([]userListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, userListItem{ID: row.ID, Login: row.Login, CreatedAt: row.CreatedAt})
	}
	writeUsersJSON(w, http.StatusOK, listUsersResponse{Items: items})
}

func writeUsersJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
