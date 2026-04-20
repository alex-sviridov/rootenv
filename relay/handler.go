package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
	"golang.org/x/crypto/ssh"
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
	idleTimeout        time.Duration
	limiter            *connLimiter
	signer             ssh.Signer
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
		log.Warn("ws accept failed", "err", err)
		return
	}
	conn.SetReadLimit(maxMessageBytes)

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

	// Parse connection details from the server record.
	srvConn, err := parseConnection(server.Connection)
	if err != nil {
		log.Error("invalid server connection data", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	// Dial SSH.
	sshClient, err := dialSSH(srvConn, h.signer)
	if err != nil {
		log.Error("ssh dial failed", "err", err, "host", srvConn.Host)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	defer sshClient.Close()

	session, err := openShell(sshClient)
	if err != nil {
		log.Error("ssh shell failed", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	defer session.Close()

	sshStdin, err := session.StdinPipe()
	if err != nil {
		log.Error("ssh stdin pipe failed", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	sshStdout, err := session.StdoutPipe()
	if err != nil {
		log.Error("ssh stdout pipe failed", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Heartbeat: send a ping on each interval; close if pong doesn't arrive within pingTimeout.
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

	// Periodic revalidation: close if the user's PocketBase session expires.
	revalidateTicker := time.NewTicker(h.revalidateInterval)
	defer revalidateTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-revalidateTicker.C:
				if _, err := h.pb.ValidateToken(token); err != nil {
					log.Info("auth: session expired, closing connection", "err", err)
					cancel()
					_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
					return
				}
			}
		}
	}()

	runProxy(ctx, cancel, conn, sshStdin, sshStdout, proxyConfig{
		idleTimeout: h.idleTimeout,
	}, log)

	_ = conn.Close(websocket.StatusNormalClosure, "")
}
