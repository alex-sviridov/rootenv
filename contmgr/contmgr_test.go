package main

import (
	"context"
	"errors"
	"testing"
)

// --- fakes ---

type fakeK8s struct {
	ensureNetworkPolicyFunc func(ctx context.Context, p NetPolParams) error
	createPodFunc           func(ctx context.Context, p PodParams) error
	createServiceFunc       func(ctx context.Context, p PodParams) error
	waitPodRunningFunc      func(ctx context.Context, namespace, podName string) error
	execInPodFunc           func(ctx context.Context, namespace, podName string, cmd []string) error
	deletePodFunc           func(ctx context.Context, namespace, podName string) error
	deleteServiceFunc       func(ctx context.Context, namespace, svcName string) error
	deleteNetworkPolicyFunc func(ctx context.Context, namespace, netpolName string) error
}

func (f *fakeK8s) EnsureNetworkPolicy(ctx context.Context, p NetPolParams) error {
	if f.ensureNetworkPolicyFunc != nil {
		return f.ensureNetworkPolicyFunc(ctx, p)
	}
	return nil
}

func (f *fakeK8s) CreatePod(ctx context.Context, p PodParams) error {
	if f.createPodFunc != nil {
		return f.createPodFunc(ctx, p)
	}
	return nil
}

func (f *fakeK8s) CreateService(ctx context.Context, p PodParams) error {
	if f.createServiceFunc != nil {
		return f.createServiceFunc(ctx, p)
	}
	return nil
}

func (f *fakeK8s) WaitPodRunning(ctx context.Context, namespace, name string) error {
	if f.waitPodRunningFunc != nil {
		return f.waitPodRunningFunc(ctx, namespace, name)
	}
	return nil
}

func (f *fakeK8s) ExecInPod(ctx context.Context, namespace, name string, cmd []string) error {
	if f.execInPodFunc != nil {
		return f.execInPodFunc(ctx, namespace, name, cmd)
	}
	return nil
}

func (f *fakeK8s) DeletePod(ctx context.Context, namespace, name string) error {
	if f.deletePodFunc != nil {
		return f.deletePodFunc(ctx, namespace, name)
	}
	return nil
}

func (f *fakeK8s) DeleteService(ctx context.Context, namespace, name string) error {
	if f.deleteServiceFunc != nil {
		return f.deleteServiceFunc(ctx, namespace, name)
	}
	return nil
}

func (f *fakeK8s) DeleteNetworkPolicy(ctx context.Context, namespace, name string) error {
	if f.deleteNetworkPolicyFunc != nil {
		return f.deleteNetworkPolicyFunc(ctx, namespace, name)
	}
	return nil
}


type fakePB struct {
	assets       map[string]*Asset
	assetConfigs map[string]*AssetConfig // keyed by assetID
	keys         map[string]*KeysRecord  // keyed by assetID
	commands     map[string]*Command
	attempts     map[string]*AttemptRecord

	patchAssetCalls       []patchCall
	patchAssetConfigCalls []patchCall
	patchKeysCalls        []patchCall
	patchCommandCalls     []patchCall
}

type patchCall struct {
	id     string
	fields map[string]any
}

func newFakePB() *fakePB {
	return &fakePB{
		assets:       make(map[string]*Asset),
		assetConfigs: make(map[string]*AssetConfig),
		keys:         make(map[string]*KeysRecord),
		commands:     make(map[string]*Command),
		attempts:     make(map[string]*AttemptRecord),
	}
}

func (f *fakePB) addAsset(a Asset) { f.assets[a.ID] = &a }
func (f *fakePB) addAssetConfig(assetID string, c AssetConfig) {
	c2 := c
	f.assetConfigs[assetID] = &c2
}
func (f *fakePB) addKeys(assetID string, k KeysRecord) {
	k2 := k
	f.keys[assetID] = &k2
}
func (f *fakePB) addCommand(c Command) { f.commands[c.ID] = &c }
func (f *fakePB) addAttempt(a AttemptRecord) { f.attempts[a.ID] = &a }

func (f *fakePB) ListPendingAssets() ([]Asset, error) {
	var out []Asset
	for _, a := range f.assets {
		if a.State == "pending" {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakePB) GetAsset(id string) (*Asset, error) {
	a, ok := f.assets[id]
	if !ok {
		return nil, errors.New("not found: " + id)
	}
	return a, nil
}

func (f *fakePB) GetAssetConfig(assetID string) (*AssetConfig, error) {
	c, ok := f.assetConfigs[assetID]
	if !ok {
		return nil, errors.New("no asset config for " + assetID)
	}
	return c, nil
}

func (f *fakePB) GetKeysByAsset(assetID string) (*KeysRecord, error) {
	k, ok := f.keys[assetID]
	if !ok {
		return nil, errors.New("no keys for " + assetID)
	}
	return k, nil
}

func (f *fakePB) GetAttempt(attemptID string) (*AttemptRecord, error) {
	a, ok := f.attempts[attemptID]
	if !ok {
		return nil, errors.New("no attempt for " + attemptID)
	}
	return a, nil
}

func (f *fakePB) PatchAsset(id string, fields map[string]any) error {
	f.patchAssetCalls = append(f.patchAssetCalls, patchCall{id, fields})
	if a, ok := f.assets[id]; ok {
		if s, ok := fields["state"].(string); ok {
			a.State = s
		}
	}
	return nil
}

func (f *fakePB) PatchAssetConfig(id string, fields map[string]any) error {
	f.patchAssetConfigCalls = append(f.patchAssetConfigCalls, patchCall{id, fields})
	return nil
}

func (f *fakePB) PatchKeys(id string, fields map[string]any) error {
	f.patchKeysCalls = append(f.patchKeysCalls, patchCall{id, fields})
	return nil
}

func (f *fakePB) ListPendingDecommissionCommands() ([]Command, error) {
	var out []Command
	for _, c := range f.commands {
		if c.Status == "pending" {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *fakePB) PatchCommand(id string, fields map[string]any) error {
	f.patchCommandCalls = append(f.patchCommandCalls, patchCall{id, fields})
	if c, ok := f.commands[id]; ok {
		if s, ok := fields["status"].(string); ok {
			c.Status = s
		}
	}
	return nil
}

func (f *fakePB) ListProvisioningAssets() ([]Asset, error) {
	var out []Asset
	for _, a := range f.assets {
		if a.State == "provisioning" {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakePB) ListDecommissioningAssets() ([]Asset, error) {
	var out []Asset
	for _, a := range f.assets {
		if a.State == "decommissioning" {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakePB) ListProvisionedAssetsByAttempt(attemptID string) ([]Asset, error) {
	var out []Asset
	for _, a := range f.assets {
		if a.Attempt == attemptID && (a.State == "provisioned" || a.State == "provisioning") {
			out = append(out, *a)
		}
	}
	return out, nil
}

// --- helpers ---

func newTestContmgr(pb pbDoer, k8s k8sDoer) *Contmgr {
	return &Contmgr{pb: pb, k8s: k8s, namespace: "rootenv-users", infraNamespace: "rootenv-infra"}
}

func addProvisionFixtures(pb *fakePB, assetID, attemptID, name, userID string) {
	pb.addAsset(Asset{ID: assetID, Attempt: attemptID, Name: name, State: "pending"})
	pb.addAttempt(AttemptRecord{ID: attemptID, User: userID})
	pb.addAssetConfig(assetID, AssetConfig{
		ID:            assetID + "-cfg",
		Asset:         assetID,
		Platform:      "container",
		Configuration: []byte(`{"image":"alpine","ssh_user":"lab","cpu":"1","memory":"128MB"}`),
	})
	pb.addKeys(assetID, KeysRecord{ID: "keys-" + assetID, Secret: "secretsecretsecretsecretsecretsec"})
}

func addDecommissionFixtures(pb *fakePB, assetID, attemptID, name, userID, pod, svc string) {
	pb.addAsset(Asset{ID: assetID, Attempt: attemptID, Name: name, State: "provisioned"})
	pb.addAttempt(AttemptRecord{ID: attemptID, User: userID})
	pb.addAssetConfig(assetID, AssetConfig{
		ID:            assetID + "-cfg",
		Asset:         assetID,
		Platform:      "container",
		Configuration: []byte(`{"platform":"container","pod":"` + pod + `","svc":"` + svc + `","user_id":"` + userID + `"}`),
	})
}

// --- naming helpers ---

func TestPodName(t *testing.T) {
	if got := podName("user1", "attempt1", "server-0"); got != "user1-attempt1-server-0" {
		t.Fatalf("want user1-attempt1-server-0, got %s", got)
	}
}

func TestSvcName(t *testing.T) {
	if got := svcName("user1", "attempt1", "server-0"); got != "user1-attempt1-server-0-svc" {
		t.Fatalf("want user1-attempt1-server-0-svc, got %s", got)
	}
}

func TestNetpolName(t *testing.T) {
	if got := netpolName("user1", "attempt1"); got != "user1-attempt1-netpol" {
		t.Fatalf("want user1-attempt1-netpol, got %s", got)
	}
}

// --- provision: network policy created with correct user/attempt ---

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

// --- provision: pod and service created with correct names ---

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

// --- provision: network policy creation failure aborts ---

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

// --- provision: connection stored in assets_configs with service DNS and port 22 ---

func TestProvisionStoresServiceConnection(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}

	// find the patch call that sets connection on assets_configs
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

// --- decommission: pod and service deleted ---

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

// --- decommission: netpol deleted when last asset for attempt ---

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

// --- decommission: netpol kept when other assets for attempt remain ---

func TestDecommissionKeepsNetpolWhenOtherAssetsRemain(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1",
		"user1-attempt1-server-0", "user1-attempt1-server-0-svc")
	// asset2 still provisioned — same attempt
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

// --- decommission: handles missing pod/svc gracefully (empty names) ---

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
