package main

import (
	"context"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// labReconcileKey is the fixed synthetic reconcile request used for LabReconciler.
// There is no primary k8s resource; all triggers map to this single key.
var labReconcileKey = reconcile.Request{
	NamespacedName: types.NamespacedName{Name: "global"},
}

type LabReconciler struct {
	contmgr      *Contmgr
	pollInterval time.Duration
	cfg          config
}

func (r *LabReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if err := r.contmgr.RunOnce(ctx); err != nil {
		return reconcile.Result{RequeueAfter: r.pollInterval}, err
	}
	if r.contmgr.NeedsReconnect() {
		slog.Warn("PocketBase unavailable, reconnecting")
		newPB, err := authWithRetry(ctx, r.cfg)
		if err != nil {
			slog.Error("PocketBase reconnect failed", "err", err)
			return reconcile.Result{RequeueAfter: r.pollInterval}, nil
		}
		r.contmgr.SetPB(newPB)
		slog.Info("reconnected to PocketBase")
	}
	return reconcile.Result{RequeueAfter: r.pollInterval}, nil
}

func (r *LabReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// A buffered channel pre-loaded with one event fires Reconcile immediately on
	// manager start. RequeueAfter keeps the loop running without further triggers.
	startupCh := make(chan event.GenericEvent, 1)
	startupCh <- event.GenericEvent{}

	mapToGlobal := handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, _ client.Object) []reconcile.Request {
			return []reconcile.Request{labReconcileKey}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		Named("lab-reconciler").
		WatchesRawSource(source.Channel(startupCh, mapToGlobal)).
		Complete(r)
}
