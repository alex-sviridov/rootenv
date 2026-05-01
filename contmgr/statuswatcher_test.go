package main

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// --- podPhaseToStatus ---

func TestPodPhaseToStatus(t *testing.T) {
	cases := []struct {
		phase     corev1.PodPhase
		eventType watch.EventType
		want      string
	}{
		{corev1.PodRunning, watch.Modified, "running"},
		{corev1.PodPending, watch.Modified, "booting"},
		{corev1.PodPending, watch.Added, "booting"},
		{corev1.PodFailed, watch.Modified, "stopped"},
		{corev1.PodSucceeded, watch.Modified, "stopped"},
		{"", watch.Modified, "stopped"},
		// Deleted event always stops regardless of phase reported.
		{corev1.PodRunning, watch.Deleted, "stopped"},
		{corev1.PodPending, watch.Deleted, "stopped"},
	}
	for _, tc := range cases {
		got := podPhaseToStatus(tc.phase, tc.eventType)
		if got != tc.want {
			t.Errorf("podPhaseToStatus(%q, %q) = %q, want %q", tc.phase, tc.eventType, got, tc.want)
		}
	}
}

// --- statusWatcher.Run ---

// fireEvent drives one pod event through the watcher and returns after Run's
// callback returns. It does this by closing ctx after the event is delivered so
// WatchPodStatuses returns and Run exits.
func runWatcherWithEvent(t *testing.T, pb *fakePB, assetName, attemptID string, phase corev1.PodPhase, eventType watch.EventType) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	k8s := &fakeK8s{
		watchPodStatusesFunc: func(ctx context.Context, onEvent func(string, string, corev1.PodPhase, watch.EventType)) error {
			onEvent(assetName, attemptID, phase, eventType)
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	w := &statusWatcher{pb: pb, k8s: k8s}
	w.Run(ctx)
}

func TestStatusWatcherPatchesRunningOnPodRunning(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.assets["asset1"].State = "provisioned"

	runWatcherWithEvent(t, pb, "server-0", "attempt1", corev1.PodRunning, watch.Modified)

	var gotStatus string
	for _, c := range pb.patchAssetCalls {
		if s, ok := c.fields["status"].(string); ok {
			gotStatus = s
		}
	}
	if gotStatus != "running" {
		t.Errorf("want status=running, got %q", gotStatus)
	}
}

func TestStatusWatcherPatchesStoppedOnPodDeleted(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.assets["asset1"].State = "provisioned"

	runWatcherWithEvent(t, pb, "server-0", "attempt1", corev1.PodRunning, watch.Deleted)

	var gotStatus string
	for _, c := range pb.patchAssetCalls {
		if s, ok := c.fields["status"].(string); ok {
			gotStatus = s
		}
	}
	if gotStatus != "stopped" {
		t.Errorf("want status=stopped, got %q", gotStatus)
	}
}

func TestStatusWatcherSkipsRedundantPatch(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.assets["asset1"].State = "provisioned"

	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	k8s := &fakeK8s{
		watchPodStatusesFunc: func(ctx context.Context, onEvent func(string, string, corev1.PodPhase, watch.EventType)) error {
			// Fire same event twice.
			onEvent("server-0", "attempt1", corev1.PodRunning, watch.Modified)
			onEvent("server-0", "attempt1", corev1.PodRunning, watch.Modified)
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	w := &statusWatcher{pb: pb, k8s: k8s}
	// Count PatchAssetStatus calls via patchAssetCalls in fakePB.
	_ = callCount
	w.Run(ctx)

	statusPatches := 0
	for _, c := range pb.patchAssetCalls {
		if _, ok := c.fields["status"]; ok {
			statusPatches++
		}
	}
	if statusPatches != 1 {
		t.Errorf("want exactly 1 PatchAssetStatus call, got %d", statusPatches)
	}
}

func TestStatusWatcherEvictsCacheOnDelete(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.assets["asset1"].State = "provisioned"

	ctx, cancel := context.WithCancel(context.Background())

	k8s := &fakeK8s{
		watchPodStatusesFunc: func(ctx context.Context, onEvent func(string, string, corev1.PodPhase, watch.EventType)) error {
			onEvent("server-0", "attempt1", corev1.PodRunning, watch.Modified) // running → cached
			onEvent("server-0", "attempt1", corev1.PodRunning, watch.Deleted)  // deleted → cache evicted
			onEvent("server-0", "attempt1", corev1.PodRunning, watch.Modified) // running again → should patch (cache miss)
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	w := &statusWatcher{pb: pb, k8s: k8s}
	w.Run(ctx)

	statusPatches := 0
	for _, c := range pb.patchAssetCalls {
		if _, ok := c.fields["status"]; ok {
			statusPatches++
		}
	}
	// running, stopped (deleted), running — 3 distinct patches because delete evicts cache.
	if statusPatches != 3 {
		t.Errorf("want 3 PatchAssetStatus calls after cache eviction, got %d", statusPatches)
	}
}

func TestStatusWatcherIgnoresUnknownAsset(t *testing.T) {
	pb := newFakePB()
	// No fixtures added — asset lookup will fail.

	runWatcherWithEvent(t, pb, "server-0", "attempt1", corev1.PodRunning, watch.Modified)

	for _, c := range pb.patchAssetCalls {
		if _, ok := c.fields["status"]; ok {
			t.Error("PatchAssetStatus must not be called when asset is not found")
		}
	}
}
