package main

import (
	"context"
	"errors"
	"testing"
)

// --- fakes ---

type fakeDocker struct {
	createAndStartFunc func(ctx context.Context, p ContainerParams) (string, int, error)
	removeFunc         func(ctx context.Context, containerID string) error
	createNetworkFunc  func(ctx context.Context, name string) error
	removeNetworkFunc  func(ctx context.Context, name string) error
}

func (f *fakeDocker) CreateAndStart(ctx context.Context, p ContainerParams) (string, int, error) {
	if f.createAndStartFunc != nil {
		return f.createAndStartFunc(ctx, p)
	}
	return "ctr-id", 2222, nil
}

func (f *fakeDocker) Remove(ctx context.Context, containerID string) error {
	if f.removeFunc != nil {
		return f.removeFunc(ctx, containerID)
	}
	return nil
}

func (f *fakeDocker) CreateNetwork(ctx context.Context, name string) error {
	if f.createNetworkFunc != nil {
		return f.createNetworkFunc(ctx, name)
	}
	return nil
}

func (f *fakeDocker) RemoveNetwork(ctx context.Context, name string) error {
	if f.removeNetworkFunc != nil {
		return f.removeNetworkFunc(ctx, name)
	}
	return nil
}

type fakePB struct {
	assets   map[string]*Asset
	keys     map[string]*KeysRecord // keyed by assetID
	commands map[string]*Command

	patchAssetCalls   []patchCall
	patchKeysCalls    []patchCall
	patchCommandCalls []patchCall
}

type patchCall struct {
	id     string
	fields map[string]any
}

func newFakePB() *fakePB {
	return &fakePB{
		assets:   make(map[string]*Asset),
		keys:     make(map[string]*KeysRecord),
		commands: make(map[string]*Command),
	}
}

func (f *fakePB) addAsset(a Asset) { f.assets[a.ID] = &a }
func (f *fakePB) addKeys(assetID string, k KeysRecord) {
	k2 := k
	f.keys[assetID] = &k2
}
func (f *fakePB) addCommand(c Command) { f.commands[c.ID] = &c }

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

func (f *fakePB) GetKeysByAsset(assetID string) (*KeysRecord, error) {
	k, ok := f.keys[assetID]
	if !ok {
		return nil, errors.New("no keys for " + assetID)
	}
	return k, nil
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

// --- helpers ---

func newTestContmgr(pb pbDoer, docker dockerDoer) *Contmgr {
	return &Contmgr{pb: pb, docker: docker, hostIP: "127.0.0.1", waitSSH: func(string, int) error { return nil }}
}

func containerAsset(id, attemptID, name string) Asset {
	return Asset{
		ID:            id,
		Attempt:       attemptID,
		Name:          name,
		State:         "pending",
		Platform:      "container",
		Configuration: []byte(`{"image":"alpine","ssh_user":"lab","cpu":"1","memory":"128MB"}`),
	}
}

func containerAssetDecommission(id, attemptID, name, containerID string) Asset {
	return Asset{
		ID:            id,
		Attempt:       attemptID,
		Name:          name,
		State:         "provisioned",
		Platform:      "container",
		Configuration: []byte(`{"platform":"container","id":"` + containerID + `"}`),
	}
}

// --- network name ---

func TestNetworkName(t *testing.T) {
	if got := networkName("abc123"); got != "lab-abc123" {
		t.Fatalf("want lab-abc123, got %s", got)
	}
}

// --- provision: network created with correct name ---

func TestProvisionCreatesNetwork(t *testing.T) {
	pb := newFakePB()
	asset := containerAsset("asset1", "attempt1", "server-0")
	pb.addAsset(asset)
	pb.addKeys("asset1", KeysRecord{ID: "keys1", Secret: "secretsecretsecretsecretsecretsec"})

	var createdNetworks []string
	docker := &fakeDocker{
		createNetworkFunc: func(_ context.Context, name string) error {
			createdNetworks = append(createdNetworks, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, docker)
	if err := mgr.ProvisionAsset(context.Background(), asset); err != nil {
		t.Fatal(err)
	}

	if len(createdNetworks) != 1 || createdNetworks[0] != "lab-attempt1" {
		t.Fatalf("expected network lab-attempt1 to be created, got %v", createdNetworks)
	}
}

// --- provision: container joined to network with asset name as alias ---

func TestProvisionContainerJoinedToNetwork(t *testing.T) {
	pb := newFakePB()
	asset := containerAsset("asset1", "attempt1", "server-0")
	pb.addAsset(asset)
	pb.addKeys("asset1", KeysRecord{ID: "keys1", Secret: "secretsecretsecretsecretsecretsec"})

	var gotParams ContainerParams
	docker := &fakeDocker{
		createAndStartFunc: func(_ context.Context, p ContainerParams) (string, int, error) {
			gotParams = p
			return "ctr-id", 2222, nil
		},
	}

	mgr := newTestContmgr(pb, docker)
	if err := mgr.ProvisionAsset(context.Background(), asset); err != nil {
		t.Fatal(err)
	}

	if gotParams.NetworkName != "lab-attempt1" {
		t.Errorf("want NetworkName=lab-attempt1, got %q", gotParams.NetworkName)
	}
	if gotParams.ContainerName != "server-0" {
		t.Errorf("want ContainerName=server-0, got %q", gotParams.ContainerName)
	}
}

// --- provision: network create failure aborts provisioning ---

func TestProvisionNetworkCreateFailureAborts(t *testing.T) {
	pb := newFakePB()
	asset := containerAsset("asset1", "attempt1", "server-0")
	pb.addAsset(asset)
	pb.addKeys("asset1", KeysRecord{ID: "keys1", Secret: "secretsecretsecretsecretsecretsec"})

	docker := &fakeDocker{
		createNetworkFunc: func(_ context.Context, _ string) error {
			return errors.New("network error")
		},
	}

	mgr := newTestContmgr(pb, docker)
	err := mgr.ProvisionAsset(context.Background(), asset)
	if err == nil {
		t.Fatal("expected error when network creation fails")
	}
}

// --- decommission: network removed after container removed ---

func TestDecommissionRemovesNetwork(t *testing.T) {
	pb := newFakePB()
	asset := containerAssetDecommission("asset1", "attempt1", "server-0", "ctr-abc")
	pb.addAsset(asset)

	var removedNetworks []string
	var removedContainers []string
	docker := &fakeDocker{
		removeFunc: func(_ context.Context, id string) error {
			removedContainers = append(removedContainers, id)
			return nil
		},
		removeNetworkFunc: func(_ context.Context, name string) error {
			removedNetworks = append(removedNetworks, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, docker)
	if err := mgr.DecommissionAsset(context.Background(), asset); err != nil {
		t.Fatal(err)
	}

	if len(removedContainers) != 1 || removedContainers[0] != "ctr-abc" {
		t.Errorf("expected container ctr-abc removed, got %v", removedContainers)
	}
	if len(removedNetworks) != 1 || removedNetworks[0] != "lab-attempt1" {
		t.Errorf("expected network lab-attempt1 removed, got %v", removedNetworks)
	}
}

// --- decommission: network removed after container, even if container ID is empty ---

func TestDecommissionRemovesNetworkEvenWithoutContainer(t *testing.T) {
	pb := newFakePB()
	// Asset with no container ID (e.g. provision failed partway)
	asset := Asset{
		ID:            "asset1",
		Attempt:       "attempt1",
		Name:          "server-0",
		State:         "provisioned",
		Platform:      "container",
		Configuration: []byte(`{}`),
	}
	pb.addAsset(asset)

	var removedNetworks []string
	docker := &fakeDocker{
		removeNetworkFunc: func(_ context.Context, name string) error {
			removedNetworks = append(removedNetworks, name)
			return nil
		},
	}

	mgr := newTestContmgr(pb, docker)
	if err := mgr.DecommissionAsset(context.Background(), asset); err != nil {
		t.Fatal(err)
	}

	if len(removedNetworks) != 1 || removedNetworks[0] != "lab-attempt1" {
		t.Errorf("expected network lab-attempt1 removed, got %v", removedNetworks)
	}
}
