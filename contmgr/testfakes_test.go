package main

import (
	"context"
	"errors"
)

// --- fakeK8s ---

type fakeK8s struct {
	ensureNamespaceFunc       func(ctx context.Context, p NamespaceParams) error
	ensureRoleBindingFunc     func(ctx context.Context, namespace string) error
	deleteNamespaceFunc       func(ctx context.Context, namespace string) error
	ensureNetworkPolicyFunc   func(ctx context.Context, p NetPolParams) error
	ensureHeadlessServiceFunc func(ctx context.Context, namespace, assetName string) error
	createPodFunc             func(ctx context.Context, p PodParams) error
	createServiceFunc         func(ctx context.Context, p PodParams) error
	waitPodRunningFunc        func(ctx context.Context, namespace, podName string) error
	execInPodFunc             func(ctx context.Context, namespace, podName string, cmd []string) error
	deletePodFunc             func(ctx context.Context, namespace, podName string) error
	deleteServiceFunc         func(ctx context.Context, namespace, svcName string) error
	deleteNetworkPolicyFunc   func(ctx context.Context, namespace, netpolName string) error
}

func (f *fakeK8s) EnsureNamespace(ctx context.Context, p NamespaceParams) error {
	if f.ensureNamespaceFunc != nil {
		return f.ensureNamespaceFunc(ctx, p)
	}
	return nil
}
func (f *fakeK8s) EnsureRoleBinding(ctx context.Context, namespace string) error {
	if f.ensureRoleBindingFunc != nil {
		return f.ensureRoleBindingFunc(ctx, namespace)
	}
	return nil
}
func (f *fakeK8s) DeleteNamespace(ctx context.Context, namespace string) error {
	if f.deleteNamespaceFunc != nil {
		return f.deleteNamespaceFunc(ctx, namespace)
	}
	return nil
}
func (f *fakeK8s) EnsureHeadlessService(ctx context.Context, namespace, assetName string) error {
	if f.ensureHeadlessServiceFunc != nil {
		return f.ensureHeadlessServiceFunc(ctx, namespace, assetName)
	}
	return nil
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

// --- fakePB ---

type fakePB struct {
	assets       map[string]*Asset
	assetConfigs map[string]*AssetConfig // keyed by assetID
	keys         map[string]*KeysRecord  // keyed by assetID
	attempts     map[string]*AttemptRecord

	patchAssetCalls       []patchCall
	patchAssetConfigCalls []patchCall
	patchKeysCalls        []patchCall

	// optional error injection — nil means no error
	getAssetConfigErr   error
	getKeysByAssetErr   error
	patchKeysErr        error
	patchAssetConfigErr error
	// patchAssetErrOn maps a "state" value to the error PatchAsset should return
	// for that specific transition. nil map means no injection.
	patchAssetErrOn map[string]error
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

func (f *fakePB) GetAssetByNameAndAttempt(name, attemptID string) (*Asset, error) {
	for _, a := range f.assets {
		if a.Name == name && a.Attempt == attemptID {
			return a, nil
		}
	}
	return nil, errors.New("not found: name=" + name + " attempt=" + attemptID)
}

func (f *fakePB) PatchAssetStatus(id, status string) error {
	f.patchAssetCalls = append(f.patchAssetCalls, patchCall{id, map[string]any{"status": status}})
	return nil
}

func (f *fakePB) GetAssetConfig(assetID string) (*AssetConfig, error) {
	if f.getAssetConfigErr != nil {
		return nil, f.getAssetConfigErr
	}
	c, ok := f.assetConfigs[assetID]
	if !ok {
		return nil, errors.New("no asset config for " + assetID)
	}
	return c, nil
}

func (f *fakePB) GetKeysByAsset(assetID string) (*KeysRecord, error) {
	if f.getKeysByAssetErr != nil {
		return nil, f.getKeysByAssetErr
	}
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
	if s, ok := fields["state"].(string); ok {
		if f.patchAssetErrOn != nil {
			if err, ok := f.patchAssetErrOn[s]; ok {
				return err
			}
		}
	}
	f.patchAssetCalls = append(f.patchAssetCalls, patchCall{id, fields})
	if a, ok := f.assets[id]; ok {
		if s, ok := fields["state"].(string); ok {
			a.State = s
		}
	}
	return nil
}

func (f *fakePB) PatchAssetConfig(id string, fields map[string]any) error {
	if f.patchAssetConfigErr != nil {
		return f.patchAssetConfigErr
	}
	f.patchAssetConfigCalls = append(f.patchAssetConfigCalls, patchCall{id, fields})
	return nil
}

func (f *fakePB) PatchKeys(id string, fields map[string]any) error {
	if f.patchKeysErr != nil {
		return f.patchKeysErr
	}
	f.patchKeysCalls = append(f.patchKeysCalls, patchCall{id, fields})
	return nil
}

func (f *fakePB) ListAttemptsToDecommission() ([]AttemptRecord, error) {
	var out []AttemptRecord
	for _, a := range f.attempts {
		if a.DesiredState == "decommissioned" && a.CurrentState != "decommissioned" {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakePB) ListActiveAssetsByAttempt(attemptID string) ([]Asset, error) {
	var out []Asset
	for _, a := range f.assets {
		if a.Attempt == attemptID && a.State != "decommissioned" {
			out = append(out, *a)
		}
	}
	return out, nil
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

// --- test helpers ---

func newTestContmgr(pb pbDoer, k8s k8sDoer) *Contmgr {
	return &Contmgr{pb: pb, k8s: k8s, infraNamespace: "rootenv-infra"}
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

func addDecommissionFixtures(pb *fakePB, assetID, attemptID, name, userID string) {
	pb.addAsset(Asset{ID: assetID, Attempt: attemptID, Name: name, State: "provisioned"})
	pb.addAttempt(AttemptRecord{ID: attemptID, User: userID})
	ns := namespaceName(attemptID)
	pb.addAssetConfig(assetID, AssetConfig{
		ID:            assetID + "-cfg",
		Asset:         assetID,
		Platform:      "container",
		Configuration: []byte(`{"platform":"container","namespace":"` + ns + `","pod":"` + podName(name) + `","svc":"` + svcName(name) + `"}`),
	})
}
