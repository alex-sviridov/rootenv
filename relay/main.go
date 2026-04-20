package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
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
	}
}

func handleHealthz(ready bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("relay not ready: PocketBase authentication failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

func main() {
	cfg := loadConfig()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel})))

	pb, err := pbclient.NewWithCredentials(cfg.pocketbaseURL, cfg.pbUsername, cfg.pbPassword)
	ready := err == nil
	if err != nil {
		slog.Error("failed to authenticate with PocketBase", "err", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz(ready))
	mux.Handle("/{serverID}/", &relayHandler{
		pb:                 pb,
		allowedOrigins:     cfg.allowedOrigins,
		revalidateInterval: cfg.revalidateInterval,
		pingInterval:       cfg.pingInterval,
		limiter:            newConnLimiter(cfg.maxConnsPerUser),
	})

	srv := &http.Server{
		Addr:    ":" + cfg.port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
}
