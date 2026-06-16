package downstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/pocketbase"
)

// applyAsCreateOrUpdate makes the fake dynamic client's Server-Side Apply
// behave like a real apiserver for tests: create the object if it doesn't
// exist yet, otherwise overwrite it. The fake ObjectTracker doesn't implement
// apply-patch merge semantics on its own.
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

func TestToLabEnvironmentIncludesAssetSetup(t *testing.T) {
	a := pocketbase.AttemptRecord{ID: "a1", UserId: "u1", UserName: "alice", Lab: "rhcsa-lab1"}
	env := environmentSpec{
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
	}

	obj := toLabEnvironment(a, env)

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
	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClient(scheme)

	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "lab.rootenv.io/v1alpha1",
		"kind":       "LabEnvironment",
		"metadata":   map[string]any{"name": "a1"},
	}}
	if err := dyn.Tracker().Create(k8s.LabEnvironmentGVR, existing, ""); err != nil {
		t.Fatalf("Tracker().Create: %v", err)
	}

	ReconcileAttempt(context.Background(), dyn, pocketbase.AttemptRecord{
		ID:           "a1",
		DesiredState: DesiredStateDecommissioned,
	})

	_, err := dyn.Resource(k8s.LabEnvironmentGVR).Get(context.Background(), "a1", metav1.GetOptions{})
	if err == nil {
		t.Error("expected LabEnvironment to be deleted, but it still exists")
	}
}

func TestResyncAttemptsAppliesActiveAttempts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	mux.HandleFunc("/api/collections/attempts/records", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":            "a1",
					"user":          "u1",
					"lab":           "rhcsa-lab1",
					"current_state": "new",
					"desired_state": "provisioned",
					"expand": map[string]any{
						"lab": map[string]any{
							"environment": map[string]any{
								"duration": 30,
								"assets":   []map[string]any{{"name": "server-0"}},
							},
						},
					},
				},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pb, err := pocketbase.NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("newPBClient: %v", err)
	}

	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClient(scheme)
	applyAsCreateOrUpdate(dyn)

	ResyncAttempts(context.Background(), pb, dyn)

	obj, err := dyn.Resource(k8s.LabEnvironmentGVR).Get(context.Background(), "a1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get LabEnvironment a1: %v", err)
	}
	spec := obj.Object["spec"].(map[string]any)
	if spec["labId"] != "rhcsa-lab1" {
		t.Errorf("labId = %v", spec["labId"])
	}
}
