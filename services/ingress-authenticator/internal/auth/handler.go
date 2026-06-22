package auth

import (
	"log/slog"
	"net/http"
	"strings"
)

// PocketBase is the interface the handler needs.
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

// parseAttemptID extracts the attempt ID from a Traefik X-Forwarded-Uri value.
// Expected pattern: /relay/exec/<attemptId>/...
func parseAttemptID(uri string) (string, bool) {
	parts := strings.FieldsFunc(uri, func(r rune) bool { return r == '/' })
	for i, p := range parts {
		if p == "exec" && i+1 < len(parts) {
			return parts[i+1], true
		}
	}
	return "", false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("pb_auth")
	if err != nil {
		http.Error(w, "missing pb_auth cookie", http.StatusUnauthorized)
		return
	}
	token := cookie.Value

	forwardedURI := r.Header.Get("X-Forwarded-Uri")
	if forwardedURI == "" {
		http.Error(w, "missing X-Forwarded-Uri", http.StatusBadRequest)
		return
	}

	attemptID, ok := parseAttemptID(forwardedURI)
	if !ok {
		http.Error(w, "cannot parse attempt ID from X-Forwarded-Uri", http.StatusBadRequest)
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
