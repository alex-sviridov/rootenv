package main

import (
	"context"
	"log/slog"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

type statusWatcher struct {
	pb    pbDoer
	k8s   k8sDoer
	cache sync.Map // map[assetID]string — last status written to PB
}

func (w *statusWatcher) Run(ctx context.Context) {
	err := w.k8s.WatchPodStatuses(ctx, func(assetName, attemptID string, phase corev1.PodPhase, eventType watch.EventType) {
		status := podPhaseToStatus(phase, eventType)

		asset, err := w.pb.GetAssetByNameAndAttempt(assetName, attemptID)
		if err != nil {
			slog.Debug("statuswatcher: asset lookup failed", "asset", assetName, "attempt", attemptID, "err", err)
			return
		}

		if prev, ok := w.cache.Load(asset.ID); ok && prev.(string) == status {
			return
		}

		if err := w.pb.PatchAssetStatus(asset.ID, status); err != nil {
			slog.Warn("statuswatcher: patch status failed", "asset", asset.ID, "status", status, "err", err)
			return
		}

		if eventType == watch.Deleted {
			w.cache.Delete(asset.ID)
		} else {
			w.cache.Store(asset.ID, status)
		}
		slog.Info("statuswatcher: status updated", "asset", asset.ID, "status", status)
	})
	if err != nil && ctx.Err() == nil {
		slog.Error("statuswatcher: watch terminated unexpectedly", "err", err)
	}
}

func podPhaseToStatus(phase corev1.PodPhase, eventType watch.EventType) string {
	if eventType == watch.Deleted {
		return "stopped"
	}
	switch phase {
	case corev1.PodRunning:
		return "running"
	case corev1.PodPending:
		return "booting"
	default:
		return "stopped"
	}
}
