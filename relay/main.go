package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type config struct {
	port               string
	pocketbaseURL      string
	pbUsername         string
	pbPassword         string
	logLevel           slog.Level
	allowedOrigins     []string
	revalidateInterval time.Duration
	pingInterval       time.Duration
	maxConnsPerUser    int
	idleTimeout        time.Duration
}

func loadConfig() config {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	pbURL := os.Getenv("RELAY_BACKEND_URL")
	if pbURL == "" {
		pbURL = os.Getenv("POCKETBASE_URL")
	}
	if pbURL == "" {
		pbURL = "http://backend:8090"
	}
	var allowedOrigins []string
	if raw := os.Getenv("RELAY_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				allowedOrigins = append(allowedOrigins, o)
			}
		}
	}
	revalidate := 30 * time.Minute
	if raw := os.Getenv("RELAY_REVALIDATE_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			revalidate = d
		}
	}
	ping := 30 * time.Second
	if raw := os.Getenv("RELAY_PING_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			ping = d
		}
	}
	maxConns := 16
	if raw := os.Getenv("RELAY_MAX_CONNS_PER_USER"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxConns = n
		}
	}
	idle := 30 * time.Minute
	if raw := os.Getenv("RELAY_IDLE_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			idle = d
		}
	}
	return config{
		port:               port,
		pocketbaseURL:      pbURL,
		pbUsername:         os.Getenv("RELAY_BACKEND_USERNAME"),
		pbPassword:         os.Getenv("RELAY_BACKEND_PASSWORD"),
		logLevel:           level,
		allowedOrigins:     allowedOrigins,
		revalidateInterval: revalidate,
		pingInterval:       ping,
		maxConnsPerUser:    maxConns,
		idleTimeout:        idle,
	}
}

func handleHealthz(ready *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("relay not ready: PocketBase authentication failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// connectWithBackoff attempts to authenticate pb in a loop with exponential backoff
// (1s, 2s, 4s, … capped at 30s). Sets ready=true on success. Returns when ctx is cancelled.
func connectWithBackoff(ctx context.Context, pb *pbclient.Client, username, password string, ready *atomic.Bool) {
	const maxDelay = 30 * time.Second
	delay := time.Second
	for {
		err := pb.Reconnect(username, password)
		if err == nil {
			ready.Store(true)
			slog.Info("connected to PocketBase")
			return
		}
		slog.Error("PocketBase authentication failed, retrying", "err", err, "retry_in", delay)
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

func main() {
	cfg := loadConfig()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel})))

	pb := pbclient.NewUnauthenticated(cfg.pocketbaseURL)
	var ready atomic.Bool

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go connectWithBackoff(ctx, pb, cfg.pbUsername, cfg.pbPassword, &ready)

	registry := prometheus.NewRegistry()
	metrics := newRelayMetrics(registry)

	var wg sync.WaitGroup
	handler := &relayHandler{
		pb:                 pb,
		ready:              &ready,
		allowedOrigins:     cfg.allowedOrigins,
		revalidateInterval: cfg.revalidateInterval,
		pingInterval:       cfg.pingInterval,
		idleTimeout:        cfg.idleTimeout,
		limiter:            newConnLimiter(cfg.maxConnsPerUser),
		wg:                 &wg,
		metrics:            metrics,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz(&ready))
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.Handle("/{serverID}/", handler)

	srv := &http.Server{
		Addr:    ":" + cfg.port,
		Handler: mux,
	}

	go func() {
		slog.Info("relay starting", "port", cfg.port, "pocketbase_url", cfg.pocketbaseURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	// Wait for all active sessions to finish, bounded by shutdownCtx deadline.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("all sessions drained")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timeout: sessions still active")
	}
}
