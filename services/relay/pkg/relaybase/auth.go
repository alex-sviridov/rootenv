package relaybase

import (
	"context"
	"errors"
	"log/slog"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
	"nhooyr.io/websocket"
)

// AuthInfo is returned by Authenticator.Authenticate on success.
// It contains only what relaybase knows — no relay-type-specific fields.
// relay-ssh parses the additional secret from the WS message itself.
type AuthInfo struct {
	UserID string
	Server *pbclient.Server
}

// AuthMetrics is implemented by each relay type's metrics struct.
// It lets Authenticator record auth-related counters without importing relay-type packages.
type AuthMetrics interface {
	RecordAuthFailure(reason string)
	RecordAuthzFailure(reason string)
	RecordConnLimitRejection()
	RecordWSAcceptError()
}

// Authenticator holds the shared dependencies for authenticating a relay connection.
// It is constructed once in main and injected into each relay-type handler.
type Authenticator struct {
	PB          *pbclient.Client
	Limiter     *ConnLimiter
	Metrics     AuthMetrics         // nil-safe
	Reconnector *BackoffReconnector // nil-safe; notified on stale admin token
}

// Authenticate validates token against PocketBase, verifies the user owns serverID,
// and acquires a per-user connection slot. It does NOT read from conn — the caller
// must have already parsed the first WS message and extracted the token.
//
// On success: returns AuthInfo. Caller must call Limiter.Release(authInfo.UserID) when done.
// On failure: closes conn with an appropriate WS status code and returns a non-nil error.
func (a *Authenticator) Authenticate(
	ctx context.Context,
	conn *websocket.Conn,
	token string,
	serverID string,
	log *slog.Logger,
) (*AuthInfo, error) {
	userID, err := a.PB.ValidateToken(token)
	if err != nil {
		log.Warn("auth failed: invalid token", "err", err)
		if a.Metrics != nil {
			a.Metrics.RecordAuthFailure("invalid_token")
		}
		if errors.Is(err, pbclient.ErrUnauthorized) && a.Reconnector != nil {
			a.Reconnector.NotifyFailure()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return nil, err
	}
	log = log.With("user_id", userID)

	if err := a.Limiter.Acquire(userID); err != nil {
		log.Warn("security: connection limit exceeded")
		if a.Metrics != nil {
			a.Metrics.RecordConnLimitRejection()
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return nil, err
	}

	server, err := a.PB.GetServer(serverID)
	if err != nil {
		a.Limiter.Release(userID)
		if errors.Is(err, pbclient.ErrNotFound) {
			log.Warn("authz failed: server not found")
			if a.Metrics != nil {
				a.Metrics.RecordAuthzFailure("server_not_found")
			}
			_ = conn.Close(websocket.StatusPolicyViolation, "server not found")
		} else {
			if errors.Is(err, pbclient.ErrUnauthorized) && a.Reconnector != nil {
				a.Reconnector.NotifyFailure()
			}
			log.Error("get server error", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "internal error")
		}
		return nil, err
	}

	if server.User != userID {
		a.Limiter.Release(userID)
		log.Warn("authz failed: server owned by different user")
		if a.Metrics != nil {
			a.Metrics.RecordAuthzFailure("forbidden")
		}
		_ = conn.Close(websocket.StatusPolicyViolation, "forbidden")
		return nil, errors.New("forbidden")
	}

	return &AuthInfo{UserID: userID, Server: server}, nil
}
