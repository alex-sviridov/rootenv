package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pb, err := authWithRetry(ctx, cfg)
	if err != nil {
		slog.Error("PocketBase auth failed", "err", err)
		os.Exit(1)
	}
	slog.Info("authenticated with PocketBase", "url", cfg.pbURL)

	k8s, err := newK8sClient()
	if err != nil {
		slog.Error("k8s client error", "err", err)
		os.Exit(1)
	}

	mgr := NewContmgr(pb, k8s, cfg.usersNamespace, cfg.imagePullSecret)

	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	slog.Info("contmgr started", "namespace", cfg.usersNamespace, "poll_interval", cfg.pollInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		case <-ticker.C:
			if err := mgr.RunOnce(ctx); err != nil {
				slog.Error("run cycle error", "err", err)
			}
			if mgr.NeedsReconnect() {
				slog.Warn("PocketBase unavailable, reconnecting")
				newPB, err := authWithRetry(ctx, cfg)
				if err != nil {
					slog.Error("PocketBase reconnect failed", "err", err)
					continue
				}
				mgr.SetPB(newPB)
				slog.Info("reconnected to PocketBase")
			}
		}
	}
}

func authWithRetry(ctx context.Context, cfg config) (*pbClient, error) {
	var err error
	for i := 0; ; i++ {
		var pb *pbClient
		pb, err = newPBClient(cfg.pbURL, cfg.pbEmail, cfg.pbPassword)
		if err == nil {
			return pb, nil
		}
		backoff := time.Duration(1<<i) * time.Second
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
		slog.Warn("PocketBase auth attempt failed", "attempt", i+1, "backoff", backoff, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
}
