package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/config"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/downstream"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/pocketbase"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/upstream"
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

	rec := downstream.NewReconciler(dyn)

	var firstConnect atomic.Bool
	firstConnect.Store(true)

	go pb.RunAttemptSubscription(ctx, func(action string, pbRec pocketbase.AttemptRecord) {
		if pbRec.DesiredState != downstream.DesiredStateDecommissioned {
			full, err := pb.GetAttempt(ctx, pbRec.ID)
			if errors.Is(err, pocketbase.ErrNotFound) {
				log.Printf("attempt %s: not found in PocketBase, removing LabEnvironment", pbRec.ID)
				rec.ReconcileAttempt(ctx, downstream.Attempt{
					ID:                 pbRec.ID,
					DesiredState:       downstream.DesiredStateDecommissioned,
					DecommissionReason: "attempt-not-found-in-pocketbase",
				})
				return
			}
			if err != nil {
				log.Printf("attempt %s: failed to fetch attempt: %v", pbRec.ID, err)
				return
			}
			pbRec = full
		}

		a, err := pbRec.ToAttempt()
		if err != nil {
			log.Printf("attempt %s: %v", pbRec.ID, err)
			return
		}
		if a.DesiredState == downstream.DesiredStateDecommissioned && a.DecommissionReason == "" {
			a.DecommissionReason = "desired-state-decommissioned"
		}
		rec.ReconcileAttempt(ctx, a)
	}, func(ctx context.Context) {
		// Skip the first onConnect resync: PocketBase replays current state via
		// SSE events immediately after subscribing, so a resync here would
		// reconcile every attempt twice. On reconnects the replay may miss events
		// that arrived while disconnected, so the resync is still needed then.
		if firstConnect.Swap(false) {
			return
		}
		rec.ResyncAttempts(ctx, pb)
	}, subscriptionReconnectBackoff)

	upRec := upstream.NewReconciler(pb)
	go upRec.Run(ctx, dyn)

	ticker := time.NewTicker(fullResyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rec.ResyncAttempts(ctx, pb)
		}
	}
}
