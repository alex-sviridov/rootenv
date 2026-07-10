package downstream

import (
	"context"
	"fmt"
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

func newReconcilerWithFake() (*Reconciler, *fake.FakeDynamicClient, *stubPBClient) {
	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClient(scheme)
	pb := &stubPBClient{}
	return NewReconciler(dyn, pb), dyn, pb
}

func TestToLabEnvironmentIncludesAssetSetup(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	a := Attempt{
		ID:       "a1",
		UserID:   "u1",
		UserName: "alice",
		LabID:    "rhcsa-lab1",
		Environment: EnvironmentSpec{
			Duration: 60,
			Assets: []Asset{
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

func TestToLabEnvironmentIncludesExercises(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	a := Attempt{
		ID:     "a1",
		UserID: "u1",
		LabID:  "rhcsa-lab1",
		Exercises: []Exercise{
			{ID: "1.1", Description: "Create a file", Type: "term", Asset: "server-0", Template: "test -f /tmp/x"},
			{ID: "1.2", Description: "No asset filter", Type: "term", Template: "echo hi"},
		},
	}

	obj := r.toLabEnvironment(a)

	spec := obj.Object["spec"].(map[string]any)
	exercises := spec["exercises"].([]any)
	if len(exercises) != 2 {
		t.Fatalf("len(exercises) = %d, want 2", len(exercises))
	}
	first := exercises[0].(map[string]any)
	if first["id"] != "1.1" || first["description"] != "Create a file" || first["type"] != "term" ||
		first["asset"] != "server-0" || first["template"] != "test -f /tmp/x" {
		t.Errorf("unexpected exercises[0]: %+v", first)
	}
	second := exercises[1].(map[string]any)
	if second["asset"] != "" {
		t.Errorf("exercises[1].asset = %q, want empty", second["asset"])
	}
}

func TestToLabEnvironmentEmptyExercisesIsEmptySlice(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	obj := r.toLabEnvironment(Attempt{ID: "a1"})
	spec := obj.Object["spec"].(map[string]any)
	exercises := spec["exercises"].([]any)
	if len(exercises) != 0 {
		t.Errorf("exercises = %v, want empty", exercises)
	}
}

func TestReconcileAttemptDecommissionedDeletesLabEnvironment(t *testing.T) {
	r, dyn, _ := newReconcilerWithFake()

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
	listErr  error
	patched  map[string]map[string]any
}

func (s *stubPBClient) ListActiveAttempts(_ context.Context) ([]Attempt, error) {
	return s.attempts, s.listErr
}

func (s *stubPBClient) PatchAttempt(_ context.Context, id string, patch map[string]any) error {
	if s.patched == nil {
		s.patched = make(map[string]map[string]any)
	}
	s.patched[id] = patch
	return nil
}

func TestReconcileAttemptDecommissionedNotFoundPatchesPocketBase(t *testing.T) {
	r, _, pb := newReconcilerWithFake()

	r.ReconcileAttempt(context.Background(), Attempt{
		ID:           "missing",
		DesiredState: DesiredStateDecommissioned,
	})

	if pb.patched["missing"]["current_state"] != DesiredStateDecommissioned {
		t.Errorf("expected PocketBase patched with decommissioned, got %v", pb.patched["missing"])
	}
}

func TestResyncAttemptsAppliesActiveAttempts(t *testing.T) {
	r, dyn, pb := newReconcilerWithFake()
	applyAsCreateOrUpdate(dyn)

	pb.attempts = []Attempt{
		{
			ID:           "a1",
			UserID:       "u1",
			LabID:        "rhcsa-lab1",
			DesiredState: "provisioned",
			Environment: EnvironmentSpec{
				Duration: 30,
				Assets:   []Asset{{Name: "server-0"}},
			},
		},
	}

	r.ResyncAttempts(context.Background())

	obj, err := dyn.Resource(k8s.LabEnvironmentGVR).Get(context.Background(), "a1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get LabEnvironment a1: %v", err)
	}
	spec := obj.Object["spec"].(map[string]any)
	if spec["labId"] != "rhcsa-lab1" {
		t.Errorf("labId = %v", spec["labId"])
	}
}

func TestToLabEnvironmentAllSpecFields(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	a := Attempt{
		ID:       "abc123",
		UserID:   "u1",
		UserName: "alice",
		LabID:    "rhcsa-lab1",
		Environment: EnvironmentSpec{
			Duration: 90,
			Assets: []Asset{
				{
					Name:           "server-0",
					Image:          "ubuntu:22.04",
					CPU:            "500m",
					Memory:         "512Mi",
					Disk:           "10Gi",
					Setup:          "apt-get install -y vim",
					RelayProtocols: []string{"ssh", "exec"},
				},
			},
		},
	}

	obj := r.toLabEnvironment(a)

	if obj.GetName() != "abc123" {
		t.Errorf("name = %q", obj.GetName())
	}
	spec := obj.Object["spec"].(map[string]any)
	if spec["ownerId"] != "u1" {
		t.Errorf("ownerId = %v", spec["ownerId"])
	}
	if spec["ownerName"] != "alice" {
		t.Errorf("ownerName = %v", spec["ownerName"])
	}
	if spec["labId"] != "rhcsa-lab1" {
		t.Errorf("labId = %v", spec["labId"])
	}
	if spec["ttl"] != 90 {
		t.Errorf("ttl = %v", spec["ttl"])
	}
	assets := spec["assets"].([]any)
	if len(assets) != 1 {
		t.Fatalf("len(assets) = %d", len(assets))
	}
	asset := assets[0].(map[string]any)
	for field, want := range map[string]any{
		"name": "server-0", "image": "ubuntu:22.04",
		"cpu": "500m", "memory": "512Mi", "disk": "10Gi",
		"setup": "apt-get install -y vim",
	} {
		if asset[field] != want {
			t.Errorf("asset.%s = %v, want %v", field, asset[field], want)
		}
	}
	protos := asset["protocols"].([]string)
	if len(protos) != 2 || protos[0] != "ssh" || protos[1] != "exec" {
		t.Errorf("protocols = %v", protos)
	}
}

func TestToLabEnvironmentNilProtocolsBecomesEmptySlice(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	obj := r.toLabEnvironment(Attempt{
		ID: "a1",
		Environment: EnvironmentSpec{
			Assets: []Asset{{Name: "server-0", RelayProtocols: nil}},
		},
	})
	spec := obj.Object["spec"].(map[string]any)
	assets := spec["assets"].([]any)
	protocols := assets[0].(map[string]any)["protocols"].([]string)
	if protocols == nil || len(protocols) != 0 {
		t.Errorf("protocols = %v, want empty non-nil slice", protocols)
	}
}

func TestReconcileAttemptDecommissionedDoesNotPatchPocketBaseOnSuccess(t *testing.T) {
	r, dyn, pb := newReconcilerWithFake()

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

	if pb.patched != nil {
		t.Errorf("PocketBase should not be patched when LabEnvironment deleted successfully, got %v", pb.patched)
	}
}

func TestResyncAttemptsIgnoresListError(t *testing.T) {
	r, _, pb := newReconcilerWithFake()
	pb.listErr = fmt.Errorf("backend unavailable")

	// Should not panic or patch anything.
	r.ResyncAttempts(context.Background())

	if pb.patched != nil {
		t.Errorf("expected no patches on list error, got %v", pb.patched)
	}
}
