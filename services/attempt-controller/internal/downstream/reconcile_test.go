package downstream

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
)

// applyAsCreateOrUpdate makes the fake dynamic client's Server-Side Apply
// behave like a real apiserver: create if absent, overwrite if present.
func applyAsCreateOrUpdate(dyn *fake.FakeDynamicClient) {
	dyn.PrependReactor("patch", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchAction := action.(clienttesting.PatchAction)
		if patchAction.GetPatchType() != types.ApplyPatchType {
			return false, nil, nil
		}
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(patchAction.GetPatch()); err != nil {
			return true, nil, err
		}
		obj.SetName(patchAction.GetName())
		gvr := patchAction.GetResource()
		if _, err := dyn.Tracker().Get(gvr, patchAction.GetNamespace(), patchAction.GetName()); err != nil {
			if err := dyn.Tracker().Create(gvr, obj, patchAction.GetNamespace()); err != nil {
				return true, nil, err
			}
			return true, obj, nil
		}
		if err := dyn.Tracker().Update(gvr, obj, patchAction.GetNamespace()); err != nil {
			return true, nil, err
		}
		return true, obj, nil
	})
}

func newReconcilerWithFake() (*Reconciler, *fake.FakeDynamicClient) {
	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClient(scheme)
	return NewReconciler(dyn), dyn
}

func TestToLabEnvironmentIncludesAssetSetup(t *testing.T) {
	r, _ := newReconcilerWithFake()
	a := Attempt{
		ID:       "a1",
		UserID:   "u1",
		UserName: "alice",
		LabID:    "rhcsa-lab1",
		Environment: environmentSpec{
			Duration: 60,
			Assets: []asset{
				{
					Name:           "server-0",
					Image:          "ubuntu",
					CPU:            "200m",
					Memory:         "256Mi",
					Disk:           "1Gi",
					Setup:          "echo hello",
					RelayProtocols: []string{"exec"},
				},
			},
		},
	}

	obj := r.toLabEnvironment(a)

	spec := obj.Object["spec"].(map[string]any)
	assets := spec["assets"].([]any)
	if len(assets) != 1 {
		t.Fatalf("len(assets) = %d", len(assets))
	}
	got := assets[0].(map[string]any)
	if got["setup"] != "echo hello" {
		t.Errorf("setup = %q, want %q", got["setup"], "echo hello")
	}
}

func TestReconcileAttemptDecommissionedDeletesLabEnvironment(t *testing.T) {
	r, dyn := newReconcilerWithFake()

	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "lab.rootenv.io/v1alpha1",
		"kind":       "LabEnvironment",
		"metadata":   map[string]any{"name": "a1"},
	}}
	if err := dyn.Tracker().Create(k8s.LabEnvironmentGVR, existing, ""); err != nil {
		t.Fatalf("Tracker().Create: %v", err)
	}

	r.ReconcileAttempt(context.Background(), Attempt{
		ID:           "a1",
		DesiredState: DesiredStateDecommissioned,
	})

	_, err := dyn.Resource(k8s.LabEnvironmentGVR).Get(context.Background(), "a1", metav1.GetOptions{})
	if err == nil {
		t.Error("expected LabEnvironment to be deleted, but it still exists")
	}
}

// stubPBClient is a minimal PocketBaseClient implementation for tests.
type stubPBClient struct {
	attempts []Attempt
}

func (s *stubPBClient) ListActiveAttempts(_ context.Context) ([]Attempt, error) {
	return s.attempts, nil
}

func TestResyncAttemptsAppliesActiveAttempts(t *testing.T) {
	r, dyn := newReconcilerWithFake()
	applyAsCreateOrUpdate(dyn)

	pb := &stubPBClient{
		attempts: []Attempt{
			{
				ID:           "a1",
				UserID:       "u1",
				LabID:        "rhcsa-lab1",
				DesiredState: "provisioned",
				Environment: environmentSpec{
					Duration: 30,
					Assets:   []asset{{Name: "server-0"}},
				},
			},
		},
	}

	r.ResyncAttempts(context.Background(), pb)

	obj, err := dyn.Resource(k8s.LabEnvironmentGVR).Get(context.Background(), "a1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get LabEnvironment a1: %v", err)
	}
	spec := obj.Object["spec"].(map[string]any)
	if spec["labId"] != "rhcsa-lab1" {
		t.Errorf("labId = %v", spec["labId"])
	}
}
