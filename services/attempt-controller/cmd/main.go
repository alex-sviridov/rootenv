package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/config"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/downstream"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/health"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/pocketbase"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/upstream"
)

const (
	subscriptionReconnectBackoff = 5 * time.Second
	fullResyncInterval           = 5 * time.Minute
	pbConnectBackoff             = 10 * time.Second
	healthAddr                   = ":8081"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		r, err := http.Get("http://localhost" + healthAddr + "/healthz")
		if err != nil || r.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}
	
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config error:", err)
	}

	hs := &health.Server{}
	go func() {
		srv := &http.Server{Addr: healthAddr, Handler: hs.Handler()}
		log.Printf("health server listening on %s", healthAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("health server error: %v", err)
		}
	}()

	var pb *pocketbase.Client
	for {
		pb, err = pocketbase.NewClient(cfg.PbURL, cfg.PbEmail, cfg.PbPassword, cfg.TlsVerify)
		if err == nil {
			break
		}
		log.Printf("PocketBase connect failed: %v; retrying in %s", err, pbConnectBackoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(pbConnectBackoff):
		}
	}
	log.Printf("connected to PocketBase at %s", cfg.PbURL)
	hs.SetReady(true)

	dyn, err := k8s.NewClient()
	if err != nil {
		log.Fatal("k8s client failed:", err)
	}

	rec := downstream.NewReconciler(dyn, pb)

	go pb.RunAttemptSubscription(ctx, func(action string, pbRec pocketbase.AttemptRecord) {
		if pbRec.DesiredState == downstream.DesiredStateDecommissioned && pbRec.CurrentState == downstream.DesiredStateDecommissioned {
			return
		}
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
		rec.ResyncAttempts(ctx)
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
			rec.ResyncAttempts(ctx)
		}
	}
}
