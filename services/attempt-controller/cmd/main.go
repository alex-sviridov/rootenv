package main

import (
	"context"
	"errors"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/config"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/downstream"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/pocketbase"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	subscriptionReconnectBackoff = 5 * time.Second
	fullResyncInterval           = 5 * time.Minute
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config error:", err)
	}

	pb, err := pocketbase.NewClient(cfg.PbURL, cfg.PbEmail, cfg.PbPassword, cfg.TlsVerify)
	if err != nil {
		log.Fatal("PocketBase auth failed:", err)
	}
	log.Printf("connected to PocketBase at %s", cfg.PbURL)

	dyn, err := k8s.NewClient()
	if err != nil {
		log.Fatal("k8s client failed:", err)
	}

	go pb.RunAttemptSubscription(ctx, func(action string, rec pocketbase.AttemptRecord) {
		// Realtime events don't carry expanded relations; re-fetch with
		// expand=lab unless we're only deleting (which doesn't need it).
		if rec.DesiredState != downstream.DesiredStateDecommissioned {
			full, err := pb.GetAttempt(rec.ID)
			if errors.Is(err, pocketbase.ErrNotFound) {
				log.Printf("attempt %s: not found in PocketBase, removing LabEnvironment", rec.ID)
				downstream.ReconcileAttempt(ctx, dyn, pocketbase.AttemptRecord{
					ID:                 rec.ID,
					DesiredState:       downstream.DesiredStateDecommissioned,
					DecommissionReason: "attempt-not-found-in-pocketbase",
				})
				return
			}
			if err != nil {
				log.Printf("attempt %s: failed to fetch attempt: %v", rec.ID, err)
				return
			}
			rec = full
		}
		if rec.DesiredState == downstream.DesiredStateDecommissioned && rec.DecommissionReason == "" {
			rec.DecommissionReason = "desired-state-decommissioned"
		}
		downstream.ReconcileAttempt(ctx, dyn, rec)
	}, func(ctx context.Context) {
		downstream.ResyncAttempts(ctx, pb, dyn)
	}, subscriptionReconnectBackoff)

	ticker := time.NewTicker(fullResyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			downstream.ResyncAttempts(ctx, pb, dyn)
		}
	}
}
