package ssh

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
	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
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

// SSHHandler is an HTTP handler that accepts WebSocket connections, authenticates
// the user via PocketBase, then proxies terminal I/O over SSH to a lab VM.
// It implements relaybase.HealthzProvider.
type SSHHandler struct {
	Auth               *relaybase.Authenticator
	PB                 *pbclient.Client // for GetKeysByAsset and GetServerConfig post-auth
	Ready              *atomic.Bool
	AllowedOrigins     []string
	RevalidateInterval time.Duration
	PingInterval       time.Duration
	IdleTimeout        time.Duration
	WG                 *sync.WaitGroup
	Metrics            *SSHMetrics
}

// IsReady implements relaybase.HealthzProvider.
func (h *SSHHandler) IsReady() bool { return h.Ready.Load() }

// ActiveConnections implements relaybase.HealthzProvider.
func (h *SSHHandler) ActiveConnections() int { return h.Auth.Limiter.Total() }

func (h *SSHHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.Ready.Load() {
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

	h.WG.Add(1)
	defer h.WG.Done()

	acceptOpts := &websocket.AcceptOptions{}
	if len(h.AllowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.AllowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		log.Warn("ws accept failed", "err", err)
		if h.Metrics != nil {
			h.Metrics.RecordWSAcceptError()
		}
		return
	}
	conn.SetReadLimit(maxMessageBytes)

	// Read first message: "<token>\n<secret>"
	// relay-ssh is responsible for parsing this format; relaybase only sees the token.
	authCtx, authCancel := context.WithTimeout(r.Context(), authTimeout)
	_, msgBytes, err := conn.Read(authCtx)
	authCancel()
	if err != nil {
		log.Warn("auth failed: did not receive token", "err", err)
		if h.Metrics != nil {
			h.Metrics.RecordAuthFailure("no_token")
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	parts := strings.SplitN(strings.TrimSpace(string(msgBytes)), "\n", 2)
	if len(parts) != 2 {
		log.Warn("auth failed: missing secret in auth message")
		if h.Metrics != nil {
			h.Metrics.RecordAuthFailure("no_token")
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	token, secret := parts[0], parts[1]

	// Delegate token validation + server ownership check to relaybase.
	// The secret is SSH-specific and stays here.
	authInfo, err := h.Auth.Authenticate(r.Context(), conn, token, serverID, log)
	if err != nil {
		// Authenticate already closed conn with the appropriate status code.
		return
	}
	defer h.Auth.Limiter.Release(authInfo.UserID)
	log = log.With("user_id", authInfo.UserID)

	keysRec, err := h.PB.GetKeysByAsset(authInfo.Server.ID)
	if err != nil {
		log.Error("get keys error", "err", err, "server_id", authInfo.Server.ID)
		if errors.Is(err, pbclient.ErrUnauthorized) && h.Auth.Reconnector != nil {
			h.Auth.Reconnector.NotifyFailure()
		}
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}
	signer, err := decryptPrivateKey(keysRec.KeyEncrypted, secret)
	if err != nil {
		log.Error("decrypt key error", "err", err, "server_id", authInfo.Server.ID)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}
	log.Debug("key decrypted", "fingerprint", gossh.FingerprintSHA256(signer.PublicKey()), "secret_len", len(secret))

	connectedAt := time.Now()
	log = log.With("attempt_id", authInfo.Server.Attempt)
	log.Info("ws connected", "active_total", h.Auth.Limiter.Total())
	if h.Metrics != nil {
		h.Metrics.activeConnections.Inc()
	}
	closeReason := "unknown"
	defer func() {
		if h.Metrics != nil {
			h.Metrics.activeConnections.Dec()
			h.Metrics.connDuration.Observe(time.Since(connectedAt).Seconds())
			h.Metrics.connCloseReasons.WithLabelValues(closeReason).Inc()
		}
		log.Info("ws disconnected",
			"duration_s", time.Since(connectedAt).Seconds(),
			"close_reason", closeReason,
			"active_total", h.Auth.Limiter.Total(),
		)
	}()

	serverCfg, err := h.PB.GetServerConfig(authInfo.Server.ID)
	if err != nil {
		log.Error("get server config error", "err", err, "server_id", authInfo.Server.ID)
		if errors.Is(err, pbclient.ErrUnauthorized) && h.Auth.Reconnector != nil {
			h.Auth.Reconnector.NotifyFailure()
		}
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	srvConn, err := parseConnection(serverCfg.Connection)
	if err != nil {
		log.Error("invalid server connection data", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	sshClient, err := dialSSH(srvConn, signer, h.Metrics)
	if err != nil {
		log.Error("ssh dial failed", "err", err, "host", srvConn.Host)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	defer sshClient.Close()

	session, sshStdin, sshStdout, err := openShellWithPipes(sshClient, h.Metrics)
	if err != nil {
		log.Error("ssh shell failed", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "ssh unavailable")
		return
	}
	defer session.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Heartbeat: send a ping on each interval; close if pong doesn't arrive within pingTimeout.
	pingTicker := time.NewTicker(h.PingInterval)
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
	revalidateTicker := time.NewTicker(h.RevalidateInterval)
	defer revalidateTicker.Stop()
	tokenRefreshChan := make(chan string, 1)
	resizeChan := make(chan [2]uint16, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-revalidateTicker.C:
				if _, err := h.PB.ValidateToken(token); err != nil {
					log.Info("auth: session expired, closing connection", "err", err)
					cancel()
					_ = conn.Close(websocket.StatusPolicyViolation, "session expired")
					return
				}
			case newToken := <-tokenRefreshChan:
				newUserID, err := h.PB.ValidateToken(newToken)
				if err != nil {
					log.Warn("auth: token refresh failed", "err", err)
					cancel()
					_ = conn.Close(websocket.StatusPolicyViolation, "invalid token")
					return
				}
				if newUserID != authInfo.UserID {
					// Prevent token hijacking: new token must be for same user
					log.Warn("auth: token refresh user mismatch", "old_user", authInfo.UserID, "new_user", newUserID)
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
		idleTimeout: h.IdleTimeout,
		metrics:     h.Metrics,
	}, tokenRefreshChan, resizeChan, windowChangeFn, log)
	closeReason = reason

	_ = conn.Close(websocket.StatusNormalClosure, "")
}
