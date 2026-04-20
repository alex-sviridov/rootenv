package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
	"nhooyr.io/websocket"
)

// maxMessageBytes is the read limit per WebSocket message.
// 256 KB accommodates large terminal pastes and cat output; anything larger
// is likely abuse rather than normal SSH traffic.
const maxMessageBytes = 256 * 1024

// authTimeout is how long the relay waits for the client to send its auth token
// as the first message after the WebSocket upgrade.
const authTimeout = 10 * time.Second

// pingTimeout is how long to wait for a pong before treating the connection as dead.
const pingTimeout = 10 * time.Second

type relayHandler struct {
	pb                 *pbclient.Client
	allowedOrigins     []string
	revalidateInterval time.Duration
	pingInterval       time.Duration
	limiter            *connLimiter
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

	// Build logger early so every event — including handshake failures — carries
	// origin and remote for correlation. Never log the token itself.
	log := slog.With(
		"server_id", serverID,
		"remote", r.RemoteAddr,
		"origin", r.Header.Get("Origin"),
	)
	log.Info("ws handshake", "method", r.Method, "path", r.URL.Path)

	acceptOpts := &websocket.AcceptOptions{}
	if len(h.allowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.allowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		// Accept already wrote HTTP error response; origin rejection lands here too.
		log.Warn("ws accept failed", "err", err)
		return
	}
	// TODO Iteration 3: log StatusMessageTooBig close reason from the read loop
	// when it is added — the library closes automatically but we won't see it until
	// we own the read path.
	conn.SetReadLimit(maxMessageBytes)

	// Read token from the first message. The token is never passed in the URL or
	// headers so it does not appear in nginx access logs or browser history.
	authCtx, authCancel := context.WithTimeout(r.Context(), authTimeout)
	_, tokenBytes, err := conn.Read(authCtx)
	authCancel()
	if err != nil {
		log.Warn("auth failed: did not receive token", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	token := strings.TrimSpace(string(tokenBytes))

	userID, err := h.pb.ValidateToken(token)
	if err != nil {
		log.Warn("auth failed: invalid token", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	log = log.With("user_id", userID)

	if err := h.limiter.Acquire(userID); err != nil {
		log.Warn("security: connection limit exceeded")
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
	defer h.limiter.Release(userID)

	server, err := h.pb.GetServer(serverID)
	if err != nil {
		if errors.Is(err, pbclient.ErrNotFound) {
			log.Warn("authz failed: server not found")
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
			log.Warn("authz failed: attempt not found", "attempt_id", server.Attempt)
			_ = conn.Close(websocket.StatusPolicyViolation, "attempt not found")
		} else {
			log.Error("get attempt error", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "internal error")
		}
		return
	}

	if attempt.User != userID {
		log.Warn("authz failed: attempt owned by different user", "attempt_id", attempt.ID)
		_ = conn.Close(websocket.StatusPolicyViolation, "forbidden")
		return
	}

	connectedAt := time.Now()
	log = log.With("attempt_id", attempt.ID)
	log.Info("ws connected")
	defer func() {
		log.Info("ws disconnected", "duration_s", time.Since(connectedAt).Seconds())
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Heartbeat: send a ping on each interval; close if pong doesn't arrive within pingTimeout.
	// Must run concurrently with any Reader (library requirement).
	pingTicker := time.NewTicker(h.pingInterval)
	defer pingTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, pingTimeout)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					log.Warn("ws dead: ping timeout", "err", err)
					cancel()
					_ = conn.Close(websocket.StatusGoingAway, "ping timeout")
					return
				}
			}
		}
	}()

	// Drain incoming frames so the library can process control frames (close, ping/pong).
	// Without an active reader the WebSocket close frame from a tab close is never read,
	// so r.Context() is never cancelled and disconnection is never logged.
	// Iteration 3 will replace this with the real SSH stdin pump.
	go func() {
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// TODO Iteration 3: establish SSH and proxy terminal I/O
	// For now hold the connection open until client closes or session expires.
	revalidateTicker := time.NewTicker(h.revalidateInterval)
	defer revalidateTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-revalidateTicker.C:
			if _, err := h.pb.ValidateToken(token); err != nil {
				log.Info("auth: session expired, closing connection", "err", err)
				_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
		}
	}
}
