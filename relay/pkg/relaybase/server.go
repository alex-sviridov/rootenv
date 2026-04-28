package relaybase

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
)

// HealthzProvider is implemented by each relay-type handler so the shared
// healthz endpoint can report relay-specific status.
type HealthzProvider interface {
	IsReady() bool
	ActiveConnections() int
}

// HandleHealthz returns an HTTP handler that reports relay health as JSON.
// Returns 503 until the first successful PocketBase authentication; 200 thereafter.
//
// 200 response:
//
//	{"status":"ok","backend":"connected","active_connections":N}
//
// 503 response:
//
//	{"status":"starting","backend":"connecting","active_connections":0}
func HandleHealthz(p HealthzProvider) http.HandlerFunc {
	type response struct {
		Status            string `json:"status"`
		Backend           string `json:"backend"`
		ActiveConnections int    `json:"active_connections"`
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var resp response
		if p.IsReady() {
			resp = response{
				Status:            "ok",
				Backend:           "connected",
				ActiveConnections: p.ActiveConnections(),
			}
			w.WriteHeader(http.StatusOK)
		} else {
			resp = response{
				Status:  "starting",
				Backend: "connecting",
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// BackoffReconnector keeps the PocketBase admin token alive.
// It authenticates at startup (with exponential backoff) and re-authenticates
// whenever NotifyFailure is called (e.g. after a 401 from a PB API call).
//
// Active WebSocket sessions are not affected — they only need the admin token
// at the moment a new connection is authenticated; established sessions proxy
// data without touching PocketBase.
type BackoffReconnector struct {
	pb       *pbclient.Client
	username string
	password string
	ready    *atomic.Bool
	notify   chan struct{}
}

// NewBackoffReconnector creates a reconnector. Call Run in a goroutine to start it.
func NewBackoffReconnector(pb *pbclient.Client, username, password string, ready *atomic.Bool) *BackoffReconnector {
	return &BackoffReconnector{
		pb:       pb,
		username: username,
		password: password,
		ready:    ready,
		notify:   make(chan struct{}, 1),
	}
}

// NotifyFailure signals the reconnector that the admin token may be stale.
// It is safe to call from multiple goroutines; excess signals are coalesced.
func (r *BackoffReconnector) NotifyFailure() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

// Run starts the reconnect loop. Blocks until ctx is cancelled.
// On first successful authentication, sets ready=true (never reset to false).
// Subsequent failures are retried silently with exponential backoff.
func (r *BackoffReconnector) Run(ctx context.Context) {
	const maxDelay = 30 * time.Second
	const idleCheck = 5 * time.Minute

	// Startup loop: keep retrying until first auth succeeds.
	delay := time.Second
	for {
		if err := r.pb.Reconnect(r.username, r.password); err == nil {
			r.ready.Store(true)
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	// Steady-state loop: re-authenticate when notified or on periodic check.
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.notify:
			// Token may be stale; reconnect immediately with backoff.
			r.reconnectWithBackoff(ctx, maxDelay)
		case <-time.After(idleCheck):
			// Proactive refresh every 5 min to catch expiry before a user hits it.
			_ = r.pb.Reconnect(r.username, r.password)
		}
	}
}

// reconnectWithBackoff retries Reconnect until success or ctx is cancelled.
func (r *BackoffReconnector) reconnectWithBackoff(ctx context.Context, maxDelay time.Duration) {
	delay := time.Second
	for {
		if err := r.pb.Reconnect(r.username, r.password); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}
