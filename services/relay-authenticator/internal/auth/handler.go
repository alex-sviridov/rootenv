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

// parseAttemptID extracts the attempt ID from X-Forwarded-Uri.
// Expected pattern: /relay/exec/<attemptId>/...
// Validates the fixed prefix so an "exec" segment elsewhere in the path is ignored.
func parseAttemptID(uri string) (string, bool) {
	// Strip leading slash so SplitN gives ["relay","exec","<id>",...].
	parts := strings.SplitN(strings.TrimPrefix(uri, "/"), "/", 4)
	if len(parts) < 3 || parts[0] != "relay" || parts[1] != "exec" || parts[2] == "" {
		return "", false
	}
	return parts[2], true
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

	ownerID, err := h.pb.GetAttempt(token, attemptID)
	if err != nil {
		slog.Warn("attempt access denied", "attempt_id", attemptID, "err", err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Defense-in-depth: cross-check returned owner against authenticated user,
	// independent of PocketBase's viewRule configuration.
	if ownerID != userID {
		slog.Warn("attempt owner mismatch", "attempt_id", attemptID, "owner", ownerID, "user", userID)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("X-User-Id", userID)
	w.Header().Set("X-Attempt-Id", attemptID)
	w.WriteHeader(http.StatusOK)
}
