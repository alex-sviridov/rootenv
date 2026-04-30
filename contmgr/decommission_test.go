package main

import (
	"context"
	"errors"
	"testing"
)

func TestDecommissionDeletesPodAndService(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")

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
	if len(deletedPods) != 1 || deletedPods[0] != "user1-attempt1-server-0" {
		t.Errorf("expected pod user1-attempt1-server-0 deleted, got %v", deletedPods)
	}
	if len(deletedSvcs) != 1 || deletedSvcs[0] != "user1-attempt1-server-0-svc" {
		t.Errorf("expected svc user1-attempt1-server-0-svc deleted, got %v", deletedSvcs)
	}
}

func TestDecommissionDeletesNetpolWhenLastAsset(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")

	var deletedNetpols []string
	k8s := &fakeK8s{
		deleteNetworkPolicyFunc: func(_ context.Context, _, name string) error {
			deletedNetpols = append(deletedNetpols, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedNetpols) != 1 || deletedNetpols[0] != "user1-attempt1-netpol" {
		t.Errorf("expected netpol user1-attempt1-netpol deleted, got %v", deletedNetpols)
	}
}

func TestDecommissionKeepsNetpolWhenOtherAssetsRemain(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	pb.addAsset(Asset{ID: "asset2", Attempt: "attempt1", Name: "server-1", State: "provisioned"})
	pb.addAssetConfig("asset2", AssetConfig{
		ID: "asset2-cfg", Asset: "asset2",
		Configuration: []byte(`{"platform":"container","pod":"user1-attempt1-server-1","svc":"user1-attempt1-server-1-svc","user_id":"user1"}`),
	})

	var deletedNetpols []string
	k8s := &fakeK8s{
		deleteNetworkPolicyFunc: func(_ context.Context, _, name string) error {
			deletedNetpols = append(deletedNetpols, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedNetpols) != 0 {
		t.Errorf("expected netpol to be kept, but it was deleted: %v", deletedNetpols)
	}
}

func TestDecommissionHandlesMissingPodSvc(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "asset1", Attempt: "attempt1", Name: "server-0", State: "provisioned"})
	pb.addAttempt(AttemptRecord{ID: "attempt1", User: "user1"})
	pb.addAssetConfig("asset1", AssetConfig{ID: "asset1-cfg", Asset: "asset1", Configuration: []byte(`{}`)})

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
}

func TestDecommissionStateTransitions(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")

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

// GetAssetConfig failing at the top of DecommissionAsset must leave the asset
// state untouched — no k8s deletes, no state transition.
func TestDecommissionGetAssetConfigFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	pb.getAssetConfigErr = errors.New("pb unavailable")

	var k8sCalled bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			k8sCalled = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when GetAssetConfig fails")
	}
	if k8sCalled {
		t.Error("k8s must not be touched when GetAssetConfig fails")
	}
	if pb.assets["asset1"].State != "provisioned" {
		t.Errorf("want state=provisioned (unchanged), got %q", pb.assets["asset1"].State)
	}
}

// Failing to mark the asset "decommissioning" must abort before any k8s delete.
func TestDecommissionMarkDecommissioningFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
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
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")

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
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")

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

// Failing to delete the NetworkPolicy when this is the last asset must surface
// as an error. On retry, DeleteNetworkPolicy returns NotFound → nil (idempotent).
func TestDecommissionDeleteNetpolFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")

	k8s := &fakeK8s{
		deleteNetworkPolicyFunc: func(_ context.Context, _, _ string) error {
			return errors.New("k8s api error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when DeleteNetworkPolicy fails")
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
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
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

// Malformed JSON in assets_configs.configuration must not abort the decommission.
// Contmgr logs a warning, skips pod/svc delete (empty names → no-op in k8s.go),
// and still marks the asset decommissioned. Covers assets that crashed before
// configuration was written.
func TestDecommissionCorruptConfigurationJSON(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "asset1", Attempt: "attempt1", Name: "server-0", State: "provisioned"})
	pb.addAssetConfig("asset1", AssetConfig{
		ID:            "asset1-cfg",
		Asset:         "asset1",
		Configuration: []byte(`{not valid json`),
	})

	var podDeleteWithName bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, name string) error {
			if name != "" {
				podDeleteWithName = true
			}
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatalf("decommission should succeed despite corrupt config: %v", err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("want state=decommissioned, got %q", pb.assets["asset1"].State)
	}
	if podDeleteWithName {
		t.Error("DeletePod must not be called with a non-empty name when config is corrupt")
	}
}

// A sibling asset in "provisioning" state counts as active — the NetworkPolicy
// must not be deleted until all assets for the attempt are gone.
// (ListProvisionedAssetsByAttempt filters state='provisioned'||state='provisioning')
func TestDecommissionNetpolKeptForProvisioningAsset(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	pb.addAsset(Asset{ID: "asset2", Attempt: "attempt1", Name: "server-1", State: "provisioning"})

	var deletedNetpols []string
	k8s := &fakeK8s{
		deleteNetworkPolicyFunc: func(_ context.Context, _, name string) error {
			deletedNetpols = append(deletedNetpols, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(deletedNetpols) != 0 {
		t.Errorf("netpol must not be deleted while a sibling is still provisioning: %v", deletedNetpols)
	}
}
