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
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pb, err := authWithRetry(cfg, 5)
	if err != nil {
		slog.Error("PocketBase auth failed after retries", "err", err)
		os.Exit(1)
	}
	slog.Info("authenticated with PocketBase", "url", cfg.pbURL)

	docker, err := newDockerClient(cfg.dockerHost)
	if err != nil {
		slog.Error("docker client error", "err", err)
		os.Exit(1)
	}

	mgr := NewContmgr(pb, docker, cfg.hostIP)

	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	slog.Info("contmgr started", "poll_interval", cfg.pollInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return
		case <-ticker.C:
			if err := mgr.RunOnce(ctx); err != nil {
				slog.Error("run cycle error", "err", err)
			}
		}
	}
}

func authWithRetry(cfg config, attempts int) (*pbClient, error) {
	var err error
	for i := range attempts {
		var pb *pbClient
		pb, err = newPBClient(cfg.pbURL, cfg.pbEmail, cfg.pbPassword)
		if err == nil {
			return pb, nil
		}
		backoff := time.Duration(1<<i) * time.Second
		slog.Warn("PocketBase auth attempt failed", "attempt", i+1, "backoff", backoff, "err", err)
		time.Sleep(backoff)
	}
	return nil, err
}
