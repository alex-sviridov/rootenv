package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
	"nhooyr.io/websocket"
)

type relayHandler struct {
	pb *pbclient.Client
}

func (h *relayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.pb == nil {
		http.Error(w, "relay not ready", http.StatusServiceUnavailable)
		return
	}

	// Extract serverID from path: /{serverID}/
	serverID := strings.Trim(r.PathValue("serverID"), "/")
	if serverID == "" {
		http.Error(w, "missing server id", http.StatusBadRequest)
		return
	}

	token := r.Header.Get("Authorization")
	if token == "" {
		// Some WebSocket clients pass token as query param
		token = r.URL.Query().Get("token")
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // CORS handled by nginx
	})
	if err != nil {
		// Accept already wrote HTTP error response
		slog.Warn("ws accept failed", "err", err)
		return
	}

	log := slog.With("server_id", serverID, "remote", r.RemoteAddr)

	userID, err := h.pb.ValidateToken(token)
	if err != nil {
		log.Warn("token validation failed", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	server, err := h.pb.GetServer(serverID)
	if err != nil {
		if errors.Is(err, pbclient.ErrNotFound) {
			log.Warn("server not found", "user_id", userID)
			_ = conn.Close(websocket.StatusPolicyViolation, "server not found")
		} else {
			log.Error("get server error", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "internal error")
		}
		return
	}

	attempt, err := h.pb.GetAttempt(server.Attempt)
	if err != nil {
		if errors.Is(err, pbclient.ErrNotFound) {
			log.Warn("attempt not found", "attempt_id", server.Attempt)
			_ = conn.Close(websocket.StatusPolicyViolation, "attempt not found")
		} else {
			log.Error("get attempt error", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "internal error")
		}
		return
	}

	if attempt.User != userID {
		log.Warn("authorization failed", "user_id", userID, "attempt_user", attempt.User)
		_ = conn.Close(websocket.StatusPolicyViolation, "forbidden")
		return
	}

	log.Info("ws connection authorized", "user_id", userID, "attempt_id", attempt.ID)

	// TODO Iteration 3: establish SSH and proxy terminal I/O
	// For now hold the connection open until client closes.
	ctx := r.Context()
	<-ctx.Done()
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
