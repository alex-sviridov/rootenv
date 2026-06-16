package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/auth"
	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/pbclient"
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

	mux := http.NewServeMux()
	mux.Handle("/auth", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("ingress-authenticator starting", "port", port, "pb_url", pbURL, "tls_verify", tlsVerify)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	_ = srv.Shutdown(context.Background())
	slog.Info("shutdown complete")
}
