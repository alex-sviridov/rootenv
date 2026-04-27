package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
	gossh "golang.org/x/crypto/ssh"
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
	ready              *atomic.Bool
	allowedOrigins     []string
	revalidateInterval time.Duration
	pingInterval       time.Duration
	idleTimeout        time.Duration
	limiter            *connLimiter
	wg                 *sync.WaitGroup
	metrics            *relayMetrics // nil-safe
}

func (h *relayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
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

	h.wg.Add(1)
	defer h.wg.Done()

	acceptOpts := &websocket.AcceptOptions{}
	if len(h.allowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.allowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		log.Warn("ws accept failed", "err", err)
		if h.metrics != nil {
			h.metrics.wsAcceptErrors.Inc()
		}
		return
	}
	conn.SetReadLimit(maxMessageBytes)

	authCtx, authCancel := context.WithTimeout(r.Context(), authTimeout)
	_, msgBytes, err := conn.Read(authCtx)
	authCancel()
	if err != nil {
		log.Warn("auth failed: did not receive token", "err", err)
		if h.metrics != nil {
			h.metrics.authFailures.WithLabelValues("no_token").Inc()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	parts := strings.SplitN(strings.TrimSpace(string(msgBytes)), "\n", 2)
	if len(parts) != 2 {
		log.Warn("auth failed: missing secret in auth message")
		if h.metrics != nil {
			h.metrics.authFailures.WithLabelValues("no_token").Inc()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	token, secret := parts[0], parts[1]

	userID, err := h.pb.ValidateToken(token)
	if err != nil {
		log.Warn("auth failed: invalid token", "err", err)
		if h.metrics != nil {
			h.metrics.authFailures.WithLabelValues("invalid_token").Inc()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	log = log.With("user_id", userID)

	if err := h.limiter.Acquire(userID); err != nil {
		log.Warn("security: connection limit exceeded")
		if h.metrics != nil {
			h.metrics.connLimitRejections.Inc()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
	defer h.limiter.Release(userID)

	server, err := h.pb.GetServer(serverID)
	if err != nil {
		if errors.Is(err, pbclient.ErrNotFound) {
			log.Warn("authz failed: server not found")
			if h.metrics != nil {
				h.metrics.authzFailures.WithLabelValues("server_not_found").Inc()
			}
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
			if h.metrics != nil {
				h.metrics.authzFailures.WithLabelValues("server_not_found").Inc()
			}
			_ = conn.Close(websocket.StatusPolicyViolation, "attempt not found")
		} else {
			log.Error("get attempt error", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "internal error")
		}
		return
	}

	if attempt.User != userID {
		log.Warn("authz failed: attempt owned by different user", "attempt_id", attempt.ID)
		if h.metrics != nil {
			h.metrics.authzFailures.WithLabelValues("forbidden").Inc()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "forbidden")
		return
	}

	keysRec, err := h.pb.GetKeysByAsset(server.ID)
	if err != nil {
		log.Error("get keys error", "err", err, "server_id", server.ID)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}
	signer, err := decryptPrivateKey(keysRec.KeyEncrypted, secret)
	if err != nil {
		log.Error("decrypt key error", "err", err, "server_id", server.ID)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}
	log.Debug("key decrypted", "fingerprint", gossh.FingerprintSHA256(signer.PublicKey()), "secret_len", len(secret))

	connectedAt := time.Now()
	log = log.With("attempt_id", attempt.ID)
	log.Info("ws connected", "active_total", h.limiter.Total())
	if h.metrics != nil {
		h.metrics.activeConnections.Inc()
	}
	closeReason := "unknown"
	defer func() {
		if h.metrics != nil {
			h.metrics.activeConnections.Dec()
			h.metrics.connDuration.Observe(time.Since(connectedAt).Seconds())
			h.metrics.connCloseReasons.WithLabelValues(closeReason).Inc()
		}
		log.Info("ws disconnected",
			"duration_s", time.Since(connectedAt).Seconds(),
			"close_reason", closeReason,
			"active_total", h.limiter.Total(),
		)
	}()

	serverCfg, err := h.pb.GetServerConfig(server.ID)
	if err != nil {
		log.Error("get server config error", "err", err, "server_id", server.ID)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	// Parse connection details from the server config record.
	srvConn, err := parseConnection(serverCfg.Connection)
	if err != nil {
		log.Error("invalid server connection data", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	// Dial SSH.
	sshClient, err := dialSSH(srvConn, signer, h.metrics)
	if err != nil {
		log.Error("ssh dial failed", "err", err, "host", srvConn.Host)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	defer sshClient.Close()

	// Create session and open shell.
	// Get pipes before calling Shell() so they're available for proxying.
	session, sshStdin, sshStdout, err := openShellWithPipes(sshClient, h.metrics)
	if err != nil {
		log.Error("ssh shell failed", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	defer session.Close()

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
	// Also handles in-band token refresh requests from the client.
	revalidateTicker := time.NewTicker(h.revalidateInterval)
	defer revalidateTicker.Stop()
	tokenRefreshChan := make(chan string, 1)
	resizeChan := make(chan [2]uint16, 1)

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
			case newToken := <-tokenRefreshChan:
				newUserID, err := h.pb.ValidateToken(newToken)
				if err != nil {
					log.Warn("auth: token refresh failed", "err", err)
					cancel()
					_ = conn.Close(websocket.StatusPolicyViolation, "invalid token")
					return
				}
				if newUserID != userID {
					// Prevent token hijacking: new token must be for same user
					log.Warn("auth: token refresh user mismatch", "old_user", userID, "new_user", newUserID)
					cancel()
					_ = conn.Close(websocket.StatusPolicyViolation, "token mismatch")
					return
				}
				token = newToken
				log.Info("auth: token refreshed")
			}
		}
	}()

	windowChangeFn := func(rows, cols int) error {
		return session.WindowChange(rows, cols)
	}

	reason := runProxy(ctx, cancel, conn, sshStdin, sshStdout, proxyConfig{
		idleTimeout: h.idleTimeout,
		metrics:     h.metrics,
	}, tokenRefreshChan, resizeChan, windowChangeFn, log)
	closeReason = reason

	_ = conn.Close(websocket.StatusNormalClosure, "")
}
