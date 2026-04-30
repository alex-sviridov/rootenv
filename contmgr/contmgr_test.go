package main

import (
	"context"
	"errors"
	"testing"
)

func TestRunOnceProvisionsPendingAssets(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "provisioned" {
		t.Errorf("expected state=provisioned, got %q", pb.assets["asset1"].State)
	}
}

func TestRunOnceDecommissionsAttemptAssets(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.attempts["attempt1"].DesiredState = "decommissioned"
	pb.attempts["attempt1"].CurrentState = "provisioned"

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("expected state=decommissioned, got %q", pb.assets["asset1"].State)
	}
}

func TestRunOnceSkipsAlreadyDecommissionedAttempt(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "asset1", Attempt: "attempt1", Name: "server-0", State: "decommissioned"})
	pb.addAttempt(AttemptRecord{ID: "attempt1", User: "user1", DesiredState: "decommissioned", CurrentState: "decommissioned"})

	var k8sCalled bool
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			k8sCalled = true
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k8sCalled {
		t.Error("k8s must not be called for an attempt already in decommissioned state")
	}
}

func TestRunOnceResetsStuckProvisioningAssets(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "asset1", Attempt: "attempt1", Name: "server-0", State: "provisioning"})
	pb.addAttempt(AttemptRecord{ID: "attempt1", User: "user1"})

	var deletedPods []string
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, name string) error {
			deletedPods = append(deletedPods, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("expected state=pending after stuck reset, got %q", pb.assets["asset1"].State)
	}
	if len(deletedPods) == 0 {
		t.Error("expected pod cleanup for stuck provisioning asset")
	}
}

func TestRunOnceResumesStuckDecommissioningAssets(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "asset1", Attempt: "attempt1", Name: "server-0", State: "decommissioning"})
	pb.addAttempt(AttemptRecord{ID: "attempt1", User: "user1"})

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("expected state=decommissioned, got %q", pb.assets["asset1"].State)
	}
}

// If DecommissionAsset fails the asset stays in "decommissioning" so the next
// cycle's stuck-decommission recovery can retry.
func TestRunOnceDecommissionFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.attempts["attempt1"].DesiredState = "decommissioned"
	pb.attempts["attempt1"].CurrentState = "provisioned"

	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			return errors.New("k8s api unavailable")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Asset should be stuck in decommissioning for retry on next cycle.
	if pb.assets["asset1"].State != "decommissioning" {
		t.Errorf("want state=decommissioning after failed decommission, got %q", pb.assets["asset1"].State)
	}
}
