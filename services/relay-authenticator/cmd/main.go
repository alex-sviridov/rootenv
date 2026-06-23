package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/auth"
	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/pbclient"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	pbURL := os.Getenv("INGAUTH_POCKETBASE_URL")
	if pbURL == "" {
		slog.Error("INGAUTH_POCKETBASE_URL is required")
		os.Exit(1)
	}
	tlsVerify := os.Getenv("INGAUTH_POCKETBASE_TLS_VERIFY") != "false"

	port := os.Getenv("INGAUTH_PORT")
	if port == "" {
		port = "8080"
	}

	pb := pbclient.New(pbURL, tlsVerify)
	handler := auth.NewHandler(pb)

	// Reuse the pbclient's transport so TLS settings (e.g. InsecureSkipVerify) are consistent.
	readyzClient := &http.Client{Timeout: 3 * time.Second, Transport: pb.HTTPClient().Transport}
	healthURL := pb.BaseURL() + "/api/health"

	mux := http.NewServeMux()
	mux.Handle("/auth", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		resp, err := readyzClient.Get(healthURL)
		if err != nil {
			http.Error(w, "pocketbase unreachable", http.StatusServiceUnavailable)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, "pocketbase unreachable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("relay-authenticator starting", "port", port, "pb_url", pbURL, "tls_verify", tlsVerify)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	slog.Info("shutdown complete")
}
