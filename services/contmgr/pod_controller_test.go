package main

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// --- podPhaseToStatus ---

func TestPodPhaseToStatus(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{string(corev1.PodRunning), "running"},
		{string(corev1.PodPending), "booting"},
		{string(corev1.PodFailed), "stopped"},
		{string(corev1.PodSucceeded), "stopped"},
		{"Unknown", "stopped"},
		{"", "stopped"},
	}
	for _, tc := range cases {
		if got := podPhaseToStatus(tc.phase); got != tc.want {
			t.Errorf("podPhaseToStatus(%q) = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

// --- namespaceToAttemptID ---

func TestNamespaceToAttemptID(t *testing.T) {
	cases := []struct {
		ns   string
		want string
	}{
		{"rootenv-lab-attempt1", "attempt1"},
		{"rootenv-lab-abc123", "abc123"},
		{"rootenv-lab-", ""},
		{"kube-system", ""},
		{"rootenv-infra", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := namespaceToAttemptID(tc.ns); got != tc.want {
			t.Errorf("namespaceToAttemptID(%q) = %q, want %q", tc.ns, got, tc.want)
		}
	}
}

// --- UpdateAssetStatusFromPod ---

func TestUpdateAssetStatusFromPodRunning(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "a1", Attempt: "att1", Name: "server-0", State: "provisioned"})

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.UpdateAssetStatusFromPod(context.Background(), "server-0", "att1", string(corev1.PodRunning)); err != nil {
		t.Fatal(err)
	}
	if len(pb.patchAssetCalls) == 0 {
		t.Fatal("expected PatchAssetStatus to be called")
	}
	if got := pb.patchAssetCalls[0].fields["status"]; got != "running" {
		t.Errorf("expected status=running, got %q", got)
	}
}

func TestUpdateAssetStatusFromPodDeleted(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "a1", Attempt: "att1", Name: "server-0", State: "provisioned"})

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.UpdateAssetStatusFromPod(context.Background(), "server-0", "att1", ""); err != nil {
		t.Fatal(err)
	}
	if len(pb.patchAssetCalls) == 0 {
		t.Fatal("expected PatchAssetStatus to be called")
	}
	if got := pb.patchAssetCalls[0].fields["status"]; got != "stopped" {
		t.Errorf("expected status=stopped, got %q", got)
	}
}

func TestUpdateAssetStatusFromPodUnknownAsset(t *testing.T) {
	pb := newFakePB()
	mgr := newTestContmgr(pb, &fakeK8s{})
	// Asset not in PB — should silently succeed (no retry churn for orphaned pods)
	if err := mgr.UpdateAssetStatusFromPod(context.Background(), "ghost", "att1", string(corev1.PodRunning)); err != nil {
		t.Errorf("expected no error for unknown asset, got %v", err)
	}
}

// --- LabReconciler ---

func TestLabReconcilerRequeuesAfterInterval(t *testing.T) {
	pb := newFakePB()
	r := &LabReconciler{
		contmgr:      newTestContmgr(pb, &fakeK8s{}),
		pollInterval: 5 * time.Second,
		cfg:          config{pbURL: "http://localhost", pbEmail: "x", pbPassword: "y"},
	}
	result, err := r.Reconcile(context.Background(), labReconcileKey)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected RequeueAfter=5s, got %v", result.RequeueAfter)
	}
}
