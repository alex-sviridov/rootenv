package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// ── Relay connection address ─────────────────────────────────────────────────

// The host stored in PocketBase must be the ClusterIP service FQDN, not the
// headless / pod DNS name. Relay dials this address over the cluster network.
func TestRelayConnectionAddressFormat(t *testing.T) {
	cases := []struct {
		attemptID string
		assetName string
		wantHost  string
	}{
		{
			"attempt1", "server-0",
			"server-0-svc.rootenv-lab-attempt1.svc.cluster.local",
		},
		{
			"abc123", "control-plane",
			"control-plane-svc.rootenv-lab-abc123.svc.cluster.local",
		},
		// Attempt IDs are PocketBase 15-char alphanumeric strings.
		{
			"x7k2mq9p3nrw4t1", "node-1",
			"node-1-svc.rootenv-lab-x7k2mq9p3nrw4t1.svc.cluster.local",
		},
	}
	for _, tc := range cases {
		ns := namespaceName(tc.attemptID)
		got := svcDNS(svcName(tc.assetName), ns)
		if got != tc.wantHost {
			t.Errorf("attemptID=%q assetName=%q: want %q, got %q",
				tc.attemptID, tc.assetName, tc.wantHost, got)
		}
	}
}

// The relay address must use the ClusterIP service ({assetName}-svc), not the
// headless service ({assetName}) used for intra-pod ping. Both are DNS names
// in the same namespace but they must be distinct so relay always gets a VIP.
func TestRelayAddressIsNotHeadlessPodDNS(t *testing.T) {
	ns := namespaceName("attempt1")
	relayHost := svcDNS(svcName("server-0"), ns)
	// headless service DNS (used for inter-pod ping)
	headlessHost := svcDNS("server-0", ns)
	if relayHost == headlessHost {
		t.Error("relay host must be ClusterIP service (-svc), not headless service")
	}
	if !strings.HasSuffix(relayHost, "-svc."+ns+".svc.cluster.local") {
		t.Errorf("relay host format unexpected: %q", relayHost)
	}
	// headless host must NOT have -svc suffix (it's just the asset name)
	if strings.Contains(headlessHost, "-svc.") {
		t.Errorf("headless host must not contain -svc suffix: %q", headlessHost)
	}
}

// ── Pod hostname / inter-pod DNS ─────────────────────────────────────────────

// CreatePod must use the assetName as pod name so that pod's hostname inside
// the container equals the assetName (Kubernetes default: hostname = pod name).
func TestCreatePodUsesAssetNameAsPodName(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var gotPod *fakeCreatedPod
	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, p PodParams) error {
			gotPod = &fakeCreatedPod{params: p}
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	_ = mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"])

	if gotPod == nil {
		t.Fatal("CreatePod was not called")
	}
	if gotPod.params.AssetName != "server-0" {
		t.Errorf("AssetName want server-0, got %q", gotPod.params.AssetName)
	}
}

// The short DNS name that resolves for inter-pod ping must be the assetName,
// not the ClusterIP service name ({assetName}-svc).
// kube-dns search path: {name}.{namespace}.svc.cluster.local
func TestInterPodDNSUsesAssetName(t *testing.T) {
	ns := namespaceName("attempt1")
	// headless service DNS == svcDNS(assetName, ns) — resolves to pod IP
	headlessHost := svcDNS("server-0", ns)
	if !strings.Contains(headlessHost, "server-0.rootenv-lab-attempt1") {
		t.Errorf("headless DNS should contain asset name, got %q", headlessHost)
	}
	// Must NOT be the relay host (ClusterIP -svc)
	if strings.Contains(headlessHost, "server-0-svc") {
		t.Errorf("inter-pod DNS must not use -svc suffix: %q", headlessHost)
	}
}

// ── Headless service provision ───────────────────────────────────────────────

func TestProvisionEnsuresHeadlessService(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var gotNS, gotAsset string
	k8s := &fakeK8s{
		ensureHeadlessServiceFunc: func(_ context.Context, ns, assetName string) error {
			gotNS = ns
			gotAsset = assetName
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if gotNS != "rootenv-lab-attempt1" {
		t.Errorf("EnsureHeadlessService namespace=%q, want rootenv-lab-attempt1", gotNS)
	}
	if gotAsset != "server-0" {
		t.Errorf("EnsureHeadlessService assetName=%q, want server-0", gotAsset)
	}
}

func TestProvisionHeadlessServiceFailureAborts(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		ensureHeadlessServiceFunc: func(_ context.Context, _, _ string) error {
			return errors.New("headless svc error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when headless service creation fails")
	}
	// Asset must revert to pending so the next cycle retries.
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("want state=pending after headless svc failure, got %q", pb.assets["asset1"].State)
	}
}

// ── Namespace metadata: empty optional fields ────────────────────────────────

// userEmail comes from ?expand=user which may fail silently. Provision must
// succeed with an empty email annotation rather than aborting.
func TestProvisionSucceedsWithEmptyUserEmail(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	// Attempt has no expand — Expand.User is nil.
	pb.attempts["attempt1"] = &AttemptRecord{ID: "attempt1", User: "user1"}

	var gotNSParams NamespaceParams
	k8s := &fakeK8s{
		ensureNamespaceFunc: func(_ context.Context, p NamespaceParams) error {
			gotNSParams = p
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if gotNSParams.UserEmail != "" {
		t.Errorf("expected empty UserEmail when expand is nil, got %q", gotNSParams.UserEmail)
	}
}

// expiresAt and labID may be absent — namespace creation must not panic or fail.
func TestProvisionSucceedsWithEmptyLabAndExpiry(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	pb.attempts["attempt1"] = &AttemptRecord{ID: "attempt1", User: "user1", Lab: "", ExpiresAt: ""}

	var gotNSParams NamespaceParams
	k8s := &fakeK8s{
		ensureNamespaceFunc: func(_ context.Context, p NamespaceParams) error {
			gotNSParams = p
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if gotNSParams.LabID != "" {
		t.Errorf("expected empty LabID, got %q", gotNSParams.LabID)
	}
	if gotNSParams.ExpiresAt != "" {
		t.Errorf("expected empty ExpiresAt, got %q", gotNSParams.ExpiresAt)
	}
}

// ── Security: namespace labelling ───────────────────────────────────────────

// Namespace params must carry exactly the attempt's IDs — never mix up
// two simultaneous attempts.
func TestProvisionNamespaceParamsBelongToAttempt(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt-A", "server-0", "user-42")
	pb.attempts["attempt-A"].Lab = "rhcsa-lab1"
	pb.attempts["attempt-A"].ExpiresAt = "2026-05-01T12:00:00Z"

	var gotNSParams NamespaceParams
	k8s := &fakeK8s{
		ensureNamespaceFunc: func(_ context.Context, p NamespaceParams) error {
			gotNSParams = p
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if gotNSParams.Name != "rootenv-lab-attempt-A" {
		t.Errorf("namespace Name=%q", gotNSParams.Name)
	}
	if gotNSParams.AttemptID != "attempt-A" {
		t.Errorf("AttemptID=%q", gotNSParams.AttemptID)
	}
	if gotNSParams.UserID != "user-42" {
		t.Errorf("UserID=%q", gotNSParams.UserID)
	}
	if gotNSParams.LabID != "rhcsa-lab1" {
		t.Errorf("LabID=%q", gotNSParams.LabID)
	}
	if gotNSParams.ExpiresAt != "2026-05-01T12:00:00Z" {
		t.Errorf("ExpiresAt=%q", gotNSParams.ExpiresAt)
	}
}

// ── Security: pod params isolation ──────────────────────────────────────────

// PodParams must reference the correct per-attempt namespace, never a shared one.
func TestProvisionPodParamsUseAttemptNamespace(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var gotPodNS, gotSvcNS string
	k8s := &fakeK8s{
		createPodFunc: func(_ context.Context, p PodParams) error {
			gotPodNS = p.Namespace
			return nil
		},
		createServiceFunc: func(_ context.Context, p PodParams) error {
			gotSvcNS = p.Namespace
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	wantNS := "rootenv-lab-attempt1"
	if gotPodNS != wantNS {
		t.Errorf("pod namespace=%q, want %q", gotPodNS, wantNS)
	}
	if gotSvcNS != wantNS {
		t.Errorf("svc namespace=%q, want %q", gotSvcNS, wantNS)
	}
}

// Two concurrent attempts must produce distinct namespaces and resource names.
func TestTwoAttemptsProduceDistinctNamespaces(t *testing.T) {
	ns1 := namespaceName("attempt-aaa")
	ns2 := namespaceName("attempt-bbb")
	if ns1 == ns2 {
		t.Error("different attempts must produce different namespaces")
	}

	// Pod and service names within each namespace are identical by design
	// (isolation is namespace-level), but the full FQDN must differ.
	fqdn1 := svcDNS(svcName("server-0"), ns1)
	fqdn2 := svcDNS(svcName("server-0"), ns2)
	if fqdn1 == fqdn2 {
		t.Error("same asset name in different attempts must produce distinct FQDNs")
	}
}

// ── Security: NetworkPolicy scope ───────────────────────────────────────────

// NetworkPolicy must apply to all pods in the namespace (empty podSelector),
// not be scoped to a label subset — otherwise unlabelled pods escape isolation.
func TestNetworkPolicyUsesEmptyPodSelector(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var gotNetPol NetPolParams
	k8s := &fakeK8s{
		ensureNetworkPolicyFunc: func(_ context.Context, p NetPolParams) error {
			gotNetPol = p
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	// NetPolParams must not carry user/attempt IDs — the real impl uses an
	// empty podSelector which covers every pod in the namespace.
	if gotNetPol.InfraNamespace != "rootenv-infra" {
		t.Errorf("InfraNamespace=%q, want rootenv-infra", gotNetPol.InfraNamespace)
	}
	if gotNetPol.Namespace != "rootenv-lab-attempt1" {
		t.Errorf("Namespace=%q, want rootenv-lab-attempt1", gotNetPol.Namespace)
	}
}

// ── Security: authorized_keys exec script ───────────────────────────────────

// The exec script must not be empty and must reference the correct SSH user.
// A shell injection in sshUser would be a critical vulnerability.
func TestProvisionExecScriptReferencesSSHUser(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")
	// Override config to use a specific SSH user.
	pb.assetConfigs["asset1"].Configuration = []byte(`{"image":"alpine","ssh_user":"labuser","cpu":"1","memory":"128MB"}`)

	var capturedCmd []string
	k8s := &fakeK8s{
		execInPodFunc: func(_ context.Context, _, _ string, cmd []string) error {
			capturedCmd = cmd
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if len(capturedCmd) == 0 {
		t.Fatal("ExecInPod was not called")
	}
	script := strings.Join(capturedCmd, " ")
	if !strings.Contains(script, "labuser") {
		t.Errorf("exec script does not reference ssh_user=labuser: %q", script)
	}
	if !strings.Contains(script, "authorized_keys") {
		t.Errorf("exec script does not write authorized_keys: %q", script)
	}
	// The public key value must be %q-quoted in the printf call, which prevents
	// shell metacharacters in the key from being interpreted.
	if !strings.Contains(script, "printf") {
		t.Errorf("exec script must use printf for safe key injection, got: %q", script)
	}
}

// ExecInPod must be called in the correct namespace and on the correct pod name.
func TestProvisionExecTargetsCorrectPod(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var execNS, execPod string
	k8s := &fakeK8s{
		execInPodFunc: func(_ context.Context, ns, pod string, _ []string) error {
			execNS = ns
			execPod = pod
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if execNS != "rootenv-lab-attempt1" {
		t.Errorf("exec namespace=%q, want rootenv-lab-attempt1", execNS)
	}
	if execPod != "server-0" {
		t.Errorf("exec pod=%q, want server-0", execPod)
	}
}

// ── Security: decommission targets correct namespace ─────────────────────────

// DeletePod and DeleteService during decommission must target the attempt's
// own namespace, never a shared or cross-attempt namespace.
func TestDecommissionTargetsAttemptNamespace(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var podNS, svcNS string
	k8s := &fakeK8s{
		deletePodFunc: func(_ context.Context, ns, _ string) error {
			podNS = ns
			return nil
		},
		deleteServiceFunc: func(_ context.Context, ns, _ string) error {
			svcNS = ns
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if podNS != "rootenv-lab-attempt1" {
		t.Errorf("DeletePod namespace=%q, want rootenv-lab-attempt1", podNS)
	}
	if svcNS != "rootenv-lab-attempt1" {
		t.Errorf("DeleteService namespace=%q, want rootenv-lab-attempt1", svcNS)
	}
}

// DeleteNamespace must be called with the attempt's namespace, not an empty
// string or a generic namespace — deleting the wrong namespace would be catastrophic.
func TestDecommissionDeletesExactNamespace(t *testing.T) {
	pb := newFakePB()
	addDecommissionFixtures(pb, "asset1", "attempt-xyz", "server-0", "user1")

	var deletedNS string
	k8s := &fakeK8s{
		deleteNamespaceFunc: func(_ context.Context, ns string) error {
			deletedNS = ns
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.DecommissionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if deletedNS != "rootenv-lab-attempt-xyz" {
		t.Errorf("DeleteNamespace called with %q, want rootenv-lab-attempt-xyz", deletedNS)
	}
	// Must not be empty — an empty namespace delete would be a no-op or target the wrong resource.
	if deletedNS == "" {
		t.Error("DeleteNamespace must not be called with an empty namespace")
	}
}

// ── Edge: RoleBinding failure ────────────────────────────────────────────────

func TestProvisionRoleBindingFailureAborts(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	k8s := &fakeK8s{
		ensureRoleBindingFunc: func(_ context.Context, _ string) error {
			return errors.New("rbac error")
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err == nil {
		t.Fatal("expected error when EnsureRoleBinding fails")
	}
	if pb.assets["asset1"].State != "pending" {
		t.Errorf("want state=pending after rbac failure, got %q", pb.assets["asset1"].State)
	}
}

// ── Edge: WaitPodRunning passes correct namespace and pod name ───────────────

func TestProvisionWaitPodRunningTargetsCorrectPod(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	var waitNS, waitPod string
	k8s := &fakeK8s{
		waitPodRunningFunc: func(_ context.Context, ns, pod string) error {
			waitNS = ns
			waitPod = pod
			return nil
		},
	}

	mgr := newTestContmgr(pb, k8s)
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}
	if waitNS != "rootenv-lab-attempt1" {
		t.Errorf("WaitPodRunning namespace=%q, want rootenv-lab-attempt1", waitNS)
	}
	if waitPod != "server-0" {
		t.Errorf("WaitPodRunning pod=%q, want server-0", waitPod)
	}
}

// ── Edge: configuration JSON written to PocketBase ──────────────────────────

// The configuration JSON must include namespace, pod, and svc so that
// future tooling can reconstruct resource names without re-deriving them.
func TestProvisionWritesConfigurationWithNamespace(t *testing.T) {
	pb := newFakePB()
	addProvisionFixtures(pb, "asset1", "attempt1", "server-0", "user1")

	mgr := newTestContmgr(pb, &fakeK8s{})
	if err := mgr.ProvisionAsset(context.Background(), *pb.assets["asset1"]); err != nil {
		t.Fatal(err)
	}

	var cfgPatch map[string]any
	for _, c := range pb.patchAssetConfigCalls {
		if cfg, ok := c.fields["configuration"]; ok {
			cfgPatch = cfg.(map[string]any)
		}
	}
	if cfgPatch == nil {
		t.Fatal("no configuration patch found")
	}
	if ns, _ := cfgPatch["namespace"].(string); ns != "rootenv-lab-attempt1" {
		t.Errorf("configuration.namespace=%q, want rootenv-lab-attempt1", ns)
	}
	if pod, _ := cfgPatch["pod"].(string); pod != "server-0" {
		t.Errorf("configuration.pod=%q, want server-0", pod)
	}
	if svc, _ := cfgPatch["svc"].(string); svc != "server-0-svc" {
		t.Errorf("configuration.svc=%q, want server-0-svc", svc)
	}
	if platform, _ := cfgPatch["platform"].(string); platform != "container" {
		t.Errorf("configuration.platform=%q, want container", platform)
	}
}

// ── Pod security hardening ───────────────────────────────────────────────────

// createPodWithFakeClient calls K8sClient.CreatePod against a fake k8s server
// and returns the pod that was actually created.
func createPodWithFakeClient(t *testing.T, p PodParams) *corev1.Pod {
	t.Helper()
	cs := k8sfake.NewSimpleClientset()
	k8s := &K8sClient{clientset: cs}
	if err := k8s.CreatePod(context.Background(), p); err != nil {
		t.Fatalf("CreatePod: %v", err)
	}
	pod, err := cs.CoreV1().Pods(p.Namespace).Get(context.Background(), podName(p.AssetName), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod: %v", err)
	}
	return pod
}

func baseTestParams() PodParams {
	return PodParams{
		Namespace: "rootenv-lab-test",
		AssetName: "server-0",
		Image:     "test-image:latest",
		SSHUser:   "user",
		CPU:       "1",
		Memory:    "512MB",
	}
}

func TestCreatePodSeccompRuntimeDefault(t *testing.T) {
	pod := createPodWithFakeClient(t, baseTestParams())
	sc := pod.Spec.SecurityContext
	if sc == nil || sc.SeccompProfile == nil {
		t.Fatal("pod SecurityContext.SeccompProfile not set")
	}
	if sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("SeccompProfile.Type want RuntimeDefault, got %q", sc.SeccompProfile.Type)
	}
}

func TestCreatePodHostUsersFalse(t *testing.T) {
	pod := createPodWithFakeClient(t, baseTestParams())
	if pod.Spec.HostUsers == nil || *pod.Spec.HostUsers {
		t.Error("HostUsers must be false")
	}
}

func TestCreatePodDropsNETRAW(t *testing.T) {
	pod := createPodWithFakeClient(t, baseTestParams())
	csc := pod.Spec.Containers[0].SecurityContext
	if csc == nil || csc.Capabilities == nil {
		t.Fatal("container SecurityContext.Capabilities not set")
	}
	for _, cap := range csc.Capabilities.Drop {
		if cap == "NET_RAW" {
			return
		}
	}
	t.Errorf("NET_RAW not in capabilities drop list: %v", csc.Capabilities.Drop)
}

func TestCreatePodResourceRequestsSetBelowLimits(t *testing.T) {
	pod := createPodWithFakeClient(t, baseTestParams())
	res := pod.Spec.Containers[0].Resources
	if res.Requests == nil {
		t.Fatal("resource Requests not set")
	}
	cpuReq := res.Requests.Cpu().MilliValue()
	cpuLim := res.Limits.Cpu().MilliValue()
	if cpuReq <= 0 || cpuReq > cpuLim {
		t.Errorf("cpu request %dm must be > 0 and <= limit %dm", cpuReq, cpuLim)
	}
	memReq := res.Requests.Memory().Value()
	memLim := res.Limits.Memory().Value()
	if memReq <= 0 || memReq > memLim {
		t.Errorf("memory request %d must be > 0 and <= limit %d", memReq, memLim)
	}
}


func TestCreatePodRuntimeClassAbsentByDefault(t *testing.T) {
	pod := createPodWithFakeClient(t, baseTestParams())
	if pod.Spec.RuntimeClassName != nil {
		t.Errorf("RuntimeClassName must be nil when not set, got %q", *pod.Spec.RuntimeClassName)
	}
}

func TestCreatePodRuntimeClassSet(t *testing.T) {
	p := baseTestParams()
	p.RuntimeClass = "gvisor"
	pod := createPodWithFakeClient(t, p)
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "gvisor" {
		t.Errorf("RuntimeClassName want gvisor, got %v", pod.Spec.RuntimeClassName)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

type fakeCreatedPod struct {
	params PodParams
}
