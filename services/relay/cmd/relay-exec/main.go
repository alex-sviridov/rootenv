package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay/exec"
	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	skipAuth := os.Getenv("RELAY_SKIP_AUTH") == "true"

	attemptID := os.Getenv("RELAY_MY_ATTEMPT_ID")
	if attemptID == "" && !skipAuth {
		slog.Error("RELAY_MY_ATTEMPT_ID is required (set RELAY_SKIP_AUTH=true to skip auth)")
		os.Exit(1)
	}
	ownerID := os.Getenv("RELAY_MY_OWNER_ID")
	if ownerID == "" && !skipAuth {
		slog.Error("RELAY_MY_OWNER_ID is required (set RELAY_SKIP_AUTH=true to skip auth)")
		os.Exit(1)
	}
	namespace := os.Getenv("RELAY_MY_NAMESPACE")
	if namespace == "" {
		slog.Error("RELAY_MY_NAMESPACE is required")
		os.Exit(1)
	}

	port := os.Getenv("RELAY_PORT")
	if port == "" {
		port = "8080"
	}

	var origins []string
	if raw := os.Getenv("RELAY_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}

	kubeExecer, err := exec.NewKubeExecer()
	if err != nil {
		slog.Error("failed to create kube execer", "err", err)
		os.Exit(1)
	}

	backend := exec.Backend{Namespace: namespace, Execer: kubeExecer}
	limiter := relaybase.NewConnLimiter(16)

	var wg sync.WaitGroup
	handler := &relaybase.Handler{
		Backend:        &backend,
		Limiter:        limiter,
		AttemptID:      attemptID,
		OwnerID:        ownerID,
		SkipAuth:       skipAuth,
		AllowedOrigins: origins,
		WG:             &wg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("/{assetName}/", handler)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("relay-exec starting", "port", port, "skip_auth", skipAuth, "attempt_id", attemptID, "namespace", namespace)
	go func() {
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
