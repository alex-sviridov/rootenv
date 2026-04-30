package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type healthResponse struct {
	Status      string `json:"status"`
	LastPollAgo string `json:"last_poll_ago,omitempty"`
	PBConnected bool   `json:"pb_connected"`
}

// newHealthMux returns a ServeMux with GET /healthz registered.
// Extracted so tests can drive the handler directly via httptest.NewRecorder.
func newHealthMux(mgr *Contmgr, staleAfter time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		lastNano := mgr.lastPollAt.Load()
		pbOK := mgr.pbHealthy.Load()

		status := "ok"
		code := http.StatusOK
		var lastPollAgo string

		switch {
		case lastNano == 0:
			status = "starting"
			code = http.StatusServiceUnavailable
		case time.Since(time.Unix(0, lastNano)) > staleAfter:
			status = "unhealthy"
			code = http.StatusServiceUnavailable
			lastPollAgo = time.Since(time.Unix(0, lastNano)).Truncate(time.Millisecond).String()
		default:
			lastPollAgo = time.Since(time.Unix(0, lastNano)).Truncate(time.Millisecond).String()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status:      status,
			LastPollAgo: lastPollAgo,
			PBConnected: pbOK,
		})
	})
	return mux
}

// startHealthServer runs an HTTP server on addr that exposes /healthz.
// It blocks until ctx is cancelled, then shuts down gracefully.
// staleAfter is how long without a completed poll before the check fails.
func startHealthServer(ctx context.Context, addr string, mgr *Contmgr, staleAfter time.Duration) {
	srv := &http.Server{
		Addr:         addr,
		Handler:      newHealthMux(mgr, staleAfter),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Warn("health server shutdown", "err", err)
		}
	}()

	slog.Info("health server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("health server error", "err", err)
	}
}
