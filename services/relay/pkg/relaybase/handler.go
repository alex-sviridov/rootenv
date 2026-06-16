package relaybase

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Backend is implemented by each relay type (exec, http, etc.).
// Called after the generic handler completes auth; responsible for the actual proxy.
type Backend interface {
	Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error
}

// Handler is a generic HTTP handler for relay types that receive auth via injected headers.
// It accepts a WebSocket upgrade, reads the first message (token placeholder, discarded),
// validates X-Attempt-Id and X-User-Id headers, acquires a connection slot, then calls Backend.
type Handler struct {
	Backend        Backend
	Limiter        *ConnLimiter
	AttemptID      string        // MY_ATTEMPT_ID — set from env at startup
	OwnerID        string        // MY_OWNER_ID — set from env at startup
	AllowedOrigins []string
	AuthTimeout    time.Duration // how long to wait for first WS message; defaults to 10s
	WG             *sync.WaitGroup
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// PathValue is set when registered with a named pattern (e.g. "/{assetName}/").
	// Fall back to extracting the first path segment for plain HandlerFunc usage.
	assetName := strings.Trim(r.PathValue("assetName"), "/")
	if assetName == "" {
		assetName = strings.Trim(strings.SplitN(r.URL.Path, "/", 3)[1], "/")
	}
	if assetName == "" {
		http.Error(w, "missing asset name", http.StatusBadRequest)
		return
	}

	log := slog.With("asset", assetName, "remote", r.RemoteAddr)

	acceptOpts := &websocket.AcceptOptions{}
	if len(h.AllowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.AllowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		log.Warn("ws accept failed", "err", err)
		return
	}

	authTimeout := h.AuthTimeout
	if authTimeout == 0 {
		authTimeout = 10 * time.Second
	}
	authCtx, authCancel := context.WithTimeout(r.Context(), authTimeout)
	_, _, err = conn.Read(authCtx)
	authCancel()
	if err != nil {
		log.Warn("auth failed: no first message received", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	attemptID := r.Header.Get("X-Attempt-Id")
	userID := r.Header.Get("X-User-Id")

	if attemptID != h.AttemptID {
		log.Warn("security: X-Attempt-Id mismatch", "got", attemptID, "want", h.AttemptID)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	if userID == "" {
		log.Warn("security: missing X-User-Id")
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	if err := h.Limiter.Acquire(userID); err != nil {
		log.Warn("connection limit exceeded", "user_id", userID)
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
	defer h.Limiter.Release(userID)

	log = log.With("user_id", userID, "attempt_id", attemptID)
	log.Info("ws connected", "active_total", h.Limiter.Total())

	if h.WG != nil {
		h.WG.Add(1)
		defer h.WG.Done()
	}

	if err := h.Backend.Serve(r.Context(), conn, assetName, userID); err != nil {
		log.Error("backend error", "err", err)
	}

	log.Info("ws disconnected")
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
