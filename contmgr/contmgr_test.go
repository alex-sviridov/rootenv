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

func TestRunOnceProcessesDecommissionCommand(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	pb.commands["cmd1"] = &Command{ID: "cmd1", Asset: "asset1", Status: "pending"}

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("expected state=decommissioned, got %q", pb.assets["asset1"].State)
	}
	if pb.commands["cmd1"].Status != "done" {
		t.Errorf("expected command status=done, got %q", pb.commands["cmd1"].Status)
	}
}

func TestRunOnceSkipsAlreadyDecommissionedAsset(t *testing.T) {
	pb := newFakePB()
	pb.addAsset(Asset{ID: "asset1", Attempt: "attempt1", Name: "server-0", State: "decommissioned"})
	pb.commands["cmd1"] = &Command{ID: "cmd1", Asset: "asset1", Status: "pending"}

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.commands["cmd1"].Status != "done" {
		t.Errorf("expected command status=done for already-decommissioned asset, got %q", pb.commands["cmd1"].Status)
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
	pb.addAssetConfig("asset1", AssetConfig{
		ID: "asset1-cfg", Asset: "asset1",
		Configuration: []byte(`{"platform":"container","pod":"user1-attempt1-server-0","svc":"user1-attempt1-server-0-svc","user_id":"user1"}`),
	})

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("expected state=decommissioned, got %q", pb.assets["asset1"].State)
	}
}

// If the process crashed after marking a command "running" but before marking it
// "done", the command must be retried and completed on the next RunOnce cycle.
func TestRunOncePicksUpRunningCommand(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	pb.commands["cmd1"] = &Command{ID: "cmd1", Asset: "asset1", Status: "running"}
	pb.assets["asset1"].State = "decommissioning"

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.assets["asset1"].State != "decommissioned" {
		t.Errorf("expected state=decommissioned, got %q", pb.assets["asset1"].State)
	}
	if pb.commands["cmd1"].Status != "done" {
		t.Errorf("expected command status=done, got %q", pb.commands["cmd1"].Status)
	}
}

// If GetAsset fails while processing a decommission command the command stays
// in "running" so the next cycle can retry.
func TestRunOnceCommandGetAssetFails(t *testing.T) {
	pb := newFakePB()
	pb.commands["cmd1"] = &Command{ID: "cmd1", Asset: "asset1", Status: "pending"}

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.commands["cmd1"].Status != "running" {
		t.Errorf("want command status=running after GetAsset failure, got %q", pb.commands["cmd1"].Status)
	}
}

// If DecommissionAsset fails the command must stay in "running" (not "done")
// so the next cycle retries the full decommission.
func TestRunOnceCommandDecommissionFails(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	pb.commands["cmd1"] = &Command{ID: "cmd1", Asset: "asset1", Status: "pending"}

	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, _, _ string) error {
			return errors.New("k8s api unavailable")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pb.commands["cmd1"].Status != "running" {
		t.Errorf("want command status=running after failed decommission, got %q", pb.commands["cmd1"].Status)
	}
}
