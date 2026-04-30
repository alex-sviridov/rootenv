package main

import (
	"context"
	"errors"
	"testing"
)

func TestProvisionEnsuresNetworkPolicy(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var gotParams NetPolParams
	k8s := &fakeK8s{
		ensureNetworkPolicyFunc: func(_ context.Context, p NetPolParams) error {
			gotParams = p
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if gotParams.UserID != "user1" {
		t.Errorf("want UserID=user1, got %q", gotParams.UserID)
	}
	if gotParams.AttemptID != "attempt1" {
		t.Errorf("want AttemptID=attempt1, got %q", gotParams.AttemptID)
	}
	if gotParams.Namespace != "rootenv-users" {
		t.Errorf("want Namespace=rootenv-users, got %q", gotParams.Namespace)
	}
}

func TestProvisionCreatesPodAndService(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var gotPod, gotSvc PodParams
	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, p PodParams) error {
			gotPod = p
			return nil
		},
		createServiceFunc: func(_ context.Context, p PodParams) error {
			gotSvc = p
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if gotPod.UserID != "user1" || gotPod.AttemptID != "attempt1" || gotPod.AssetName != "server-0" {
		t.Errorf("pod params wrong: %+v", gotPod)
	}
	if gotSvc.UserID != "user1" || gotSvc.AttemptID != "attempt1" || gotSvc.AssetName != "server-0" {
		t.Errorf("svc params wrong: %+v", gotSvc)
	}
}

func TestProvisionNetpolFailureAborts(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		ensureNetworkPolicyFunc: func(_ context.Context, _ NetPolParams) error {
			return errors.New("netpol error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when network policy creation fails")
	}
}

func TestProvisionStoresServiceConnection(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}

	var connPatch map[string]any
	for _, c := range pb.patchAssetConfigCalls {
		if conn, ok := c.fields["connection"]; ok {
			connPatch = conn.(map[string]any)
		}
	}
	if connPatch == nil {
		t.Fatal("no connection patch found in assets_configs")
	}
	wantHost := "user1-attempt1-server-0-svc.rootenv-users.svc.cluster.local"
	if got, _ := connPatch["host"].(string); got != wantHost {
		t.Errorf("want host=%q, got %q", wantHost, got)
	}
	if got, _ := connPatch["port"].(int); got != 22 {
		t.Errorf("want port=22, got %v", connPatch["port"])
	}
}

func TestProvisionStateTransitions(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}

	var states []string
	for _, c := range pb.patchAssetCalls {
		if s, ok := c.fields["state"].(string); ok {
			states = append(states, s)
		}
	}
	if len(states) < 2 || states[0] != "provisioning" || states[len(states)-1] != "provisioned" {
		t.Errorf("unexpected state transitions: %v", states)
	}
}

func TestProvisionCreatePodFailureResetsStateToPending(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, _ PodParams) error {
			return errors.New("pod creation failed")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("expected state=pending after failure, got %q", pb.assets["asset1"].State)
	}
}

func TestProvisionWaitPodFailureResetsStateToPending(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		waitPodRunningFunc: func(_ context.Context, _, _ string) error {
			return errors.New("pod never became ready")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("expected state=pending after failure, got %q", pb.assets["asset1"].State)
	}
}

func TestProvisionExecFailureResetsStateToPending(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		execInPodFunc: func(_ context.Context, _, _ string, _ []string) error {
			return errors.New("exec failed")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("expected state=pending after failure, got %q", pb.assets["asset1"].State)
	}
}

func TestProvisionEmptySecretReturnsError(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.keys["asset1"] = &KeysRecord{ID: "keys-asset1", Secret: ""}

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestProvisionFailureTriggersK8sCleanup(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var deletedPods, deletedSvcs []string
	k8s := &fakeK8s{
		createServiceFunc: func(_ context.Context, _ PodParams) error {
			return errors.New("svc creation failed")
		},
		deletePodFunc: func(_ context.Context, _, name string) error {
			deletedPods = append(deletedPods, name)
			return nil
		},
		deleteServiceFunc: func(_ context.Context, _, name string) error {
			deletedSvcs = append(deletedSvcs, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	_ = mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"])

	if len(deletedPods) == 0 {
		t.Error("expected pod cleanup after provision failure")
	}
	if len(deletedSvcs) == 0 {
		t.Error("expected svc cleanup after provision failure")
	}
}

func TestProvisionValidatesMissingImage(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.assetConfigs["asset1"].Configuration = []byte(`{"ssh_user":"lab","cpu":"1","memory":"128MB"}`)

	var podCalled bool
	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, _ PodParams) error {
			podCalled = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error for missing image")
	}
	if podCalled {
		t.Error("CreatePod must not be called when validation fails")
	}
}

func TestProvisionValidatesMissingSSHUser(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.assetConfigs["asset1"].Configuration = []byte(`{"image":"alpine","cpu":"1","memory":"128MB"}`)

	var podCalled bool
	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, _ PodParams) error {
			podCalled = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error for missing ssh_user")
	}
	if podCalled {
		t.Error("CreatePod must not be called when validation fails")
	}
}

// Cleanup must run even when the parent context is already cancelled (e.g.
// graceful shutdown fires while provision is failing).
func TestProvisionCleanupRunsOnCancelledContext(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	ctx, cancel := context.WithCancel(context.Background())

	var deletePodCalled bool
	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, _ PodParams) error {
			cancel()
			return errors.New("pod creation failed")
		},
		deletePodFunc: func(_ context.Context, _, _ string) error {
			deletePodCalled = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	_ = mgr.ProvisionAsset(ctx, *pb.assets["asset1"])

	if !deletePodCalled {
		t.Error("expected DeletePod cleanup even after context cancellation")
	}
}

// Pod phase transitions: Failed and Succeeded mean the container exited before
// SSH keys could be configured; WatchClosed models the 120 s server-side timeout.
// All three must trigger k8s cleanup and reset the asset to "pending".
func TestProvisionPodPhaseTransitions(t *testing.T) {
	phases := []struct {
		name string
		err  string
	}{
		{"Failed", "pod user1-attempt1-server-0 ended unexpectedly: phase=Failed"},
		{"Succeeded", "pod user1-attempt1-server-0 ended unexpectedly: phase=Succeeded"},
		{"WatchClosed", "pod user1-attempt1-server-0 watch channel closed before Running"},
	}
	for _, tc := range phases {
		t.Run(tc.name, func(t *testing.T) {
			pb := newFakePB()
			addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

			var cleanedUp bool
			k8s := &fakeK8s{
				waitPodRunningFunc: func(_ context.Context, _, _ string) error {
					return errors.New(tc.err)
				},
				deletePodFunc: func(_ context.Context, _, _ string) error {
					cleanedUp = true
					return nil
				},
			}

			mgr := newTestContmgr(pb, k8s)
			if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
				t.Fatalf("expected error for pod phase %s", tc.name)
			}
			if pb.assets["asset1"].State != "pending" {
				t.Errorf("want state=pending, got %q", pb.assets["asset1"].State)
			}
			if !cleanedUp {
				t.Error("expected k8s cleanup after pod phase transition")
			}
		})
	}
}

// After ExecInPod succeeds the pod and service exist in k8s. Any subsequent PB
// failure must still trigger k8s cleanup so orphaned resources are removed.

func TestProvisionGetKeysFailsAfterExec(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.getKeysByAssetErr = errors.New("pb unavailable")

	var deletedPods, deletedSvcs []string
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, name string) error {
			deletedPods = append(deletedPods, name)
			return nil
		},
		deleteServiceFunc: func(_ context.Context, _, name string) error {
			deletedSvcs = append(deletedSvcs, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error")
	}
	if len(deletedPods) == 0 {
		t.Error("expected pod cleanup after GetKeys failure")
	}
	if len(deletedSvcs) == 0 {
		t.Error("expected svc cleanup after GetKeys failure")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("want state=pending, got %q", pb.assets["asset1"].State)
	}
}

func TestProvisionPatchKeysFailsAfterExec(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.patchKeysErr = errors.New("pb write failed")

	var cleanedUp bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			cleanedUp = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error")
	}
	if !cleanedUp {
		t.Error("expected k8s cleanup after PatchKeys failure")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("want state=pending, got %q", pb.assets["asset1"].State)
	}
}

func TestProvisionPatchAssetConfigFailsAfterExec(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.patchAssetConfigErr = errors.New("pb write failed")

	var cleanedUp bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			cleanedUp = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error")
	}
	if !cleanedUp {
		t.Error("expected k8s cleanup after PatchAssetConfig failure")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("want state=pending, got %q", pb.assets["asset1"].State)
	}
}

// Final PatchAsset("provisioned") failing is the worst case: k8s resources are
// live and the asset is stuck in "provisioning". The stuck-provisioning recovery
// handles it on the next cycle.
func TestProvisionFinalPatchFails(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.patchAssetErrOn = map[string]error{"provisioned": errors.New("pb write failed")}

	var cleanedUp bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			cleanedUp = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when final patch fails")
	}
	if !cleanedUp {
		t.Error("expected k8s cleanup after final patch failure")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("want state=pending, got %q", pb.assets["asset1"].State)
	}
}
