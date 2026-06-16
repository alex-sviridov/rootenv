package auth

import (
	"log/slog"
	"net/http"
)

// PocketBase is the interface the handler needs — two calls, both with the user token.
type PocketBase interface {
	ValidateToken(token string) (string, error)
	GetAttempt(token, attemptID string) (string, error)
}

type Handler struct {
	pb PocketBase
}

func NewHandler(pb PocketBase) *Handler {
	return &Handler{pb: pb}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if token == "" {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}

	attemptID := r.Header.Get("X-Attempt-Id")
	if attemptID == "" {
		http.Error(w, "missing X-Attempt-Id", http.StatusBadRequest)
		return
	}

	userID, err := h.pb.ValidateToken(token)
	if err != nil {
		slog.Warn("token validation failed", "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if _, err := h.pb.GetAttempt(token, attemptID); err != nil {
		slog.Warn("attempt access denied", "attempt_id", attemptID, "err", err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("X-User-Id", userID)
	w.Header().Set("X-Attempt-Id", attemptID)
	w.WriteHeader(http.StatusOK)
}
