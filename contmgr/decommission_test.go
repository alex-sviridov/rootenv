package main

import (
	"context"
	"errors"
	"testing"
)

func TestDecommissionDeletesPodAndService(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

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
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedPods) != 1 || deletedPods[0] != "server-0" {
		t.Errorf("expected pod server-0 deleted, got %v", deletedPods)
	}
	if len(deletedSvcs) != 1 || deletedSvcs[0] != "server-0-svc" {
		t.Errorf("expected svc server-0-svc deleted, got %v", deletedSvcs)
	}
}

func TestDecommissionDeletesNamespaceWhenLastAsset(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var deletedNamespaces []string
	k8s := &fakeK8s{
		deleteNamespaceFunc: func(_ context.Context, ns string) error {
			deletedNamespaces = append(deletedNamespaces, ns)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedNamespaces) != 1 || deletedNamespaces[0] != "rootenv-lab-attempt1" {
		t.Errorf("expected namespace rootenv-lab-attempt1 deleted, got %v", deletedNamespaces)
	}
}

func TestDecommissionKeepsNamespaceWhenOtherAssetsRemain(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.addAsset(Asset{ID: "asset2", Attempt: "attempt1", Name: "server-1", State: "provisioned"})

	var deletedNamespaces []string
	k8s := &fakeK8s{
		deleteNamespaceFunc: func(_ context.Context, ns string) error {
			deletedNamespaces = append(deletedNamespaces, ns)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedNamespaces) != 0 {
		t.Errorf("expected namespace to be kept, but it was deleted: %v", deletedNamespaces)
	}
}

func TestDecommissionStateTransitions(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}

	var states []string
	for _, c := range pb.patchAssetCalls {
		if s, ok := c.fields["state"].(string); ok {
			states = append(states, s)
		}
	}
	if len(states) < 2 || states[0] != "decommissioning" || states[len(states)-1] != "decommissioned" {
		t.Errorf("unexpected state transitions: %v", states)
	}
}

// Failing to mark the asset "decommissioning" must abort before any k8s delete.
func TestDecommissionMarkDecommissioningFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.patchAssetErrOn = map[string]error{"decommissioning": errors.New("pb write failed")}

	var k8sCalled bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			k8sCalled = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when PatchAsset(decommissioning) fails")
	}
	if k8sCalled {
		t.Error("k8s must not be touched when PatchAsset(decommissioning) fails")
	}
}

// DeletePod returning a non-NotFound error halts decommission; asset stays in
// "decommissioning" so the stuck-decommission recovery can retry on next cycle.
func TestDecommissionDeletePodFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			return errors.New("k8s api error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when DeletePod fails")
	}
	if pb.assets["asset1"].State != "decommissioning" {
		t.Errorf("want state=decommissioning (for retry), got %q", pb.assets["asset1"].State)
	}
}

func TestDecommissionDeleteServiceFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		deleteServiceFunc: func(_ context.Context, _, _ string) error {
			return errors.New("k8s api error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when DeleteService fails")
	}
	if pb.assets["asset1"].State != "decommissioning" {
		t.Errorf("want state=decommissioning, got %q", pb.assets["asset1"].State)
	}
}

// Failing to delete the namespace when this is the last asset must surface
// as an error. On retry, DeleteNamespace returns NotFound → nil (idempotent).
func TestDecommissionDeleteNamespaceFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		deleteNamespaceFunc: func(_ context.Context, _ string) error {
			return errors.New("k8s api error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when DeleteNamespace fails")
	}
	if pb.assets["asset1"].State != "decommissioning" {
		t.Errorf("want state=decommissioning (for retry), got %q", pb.assets["asset1"].State)
	}
}

// If the final PatchAsset("decommissioned") fails the k8s resources are already
// gone. The asset is stuck in "decommissioning". Retry must succeed because all
// k8s deletes are idempotent (NotFound → nil in the real client).
func TestDecommissionFinalPatchFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.patchAssetErrOn = map[string]error{"decommissioned": errors.New("pb write failed")}

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when final patch fails")
	}
	if pb.assets["asset1"].State != "decommissioning" {
		t.Errorf("want state=decommissioning after failed final patch, got %q", pb.assets["asset1"].State)
	}

	// Retry: clear error injection; k8s deletes return nil (models NotFound).
	pb.patchAssetErrOn = nil
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("want state=decommissioned after retry, got %q", pb.assets["asset1"].State)
	}
}

// A sibling asset in "provisioning" state counts as active — the namespace
// must not be deleted until all assets for the attempt are gone.
func TestDecommissionNamespaceKeptForProvisioningAsset(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.addAsset(Asset{ID: "asset2", Attempt: "attempt1", Name: "server-1", State: "provisioning"})

	var deletedNamespaces []string
	k8s := &fakeK8s{
		deleteNamespaceFunc: func(_ context.Context, ns string) error {
			deletedNamespaces = append(deletedNamespaces, ns)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedNamespaces) != 0 {
		t.Errorf("namespace must not be deleted while a sibling is still provisioning: %v", deletedNamespaces)
	}
}
