package upstream

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// stubWriter records calls to PatchAttempt.
type stubWriter struct {
	id    string
	patch map[string]any
}

func (s *stubWriter) PatchAttempt(_ context.Context, id string, patch map[string]any) error {
	s.id = id
	s.patch = patch
	return nil
}

func mustUnstructured(obj map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: obj}
}

func TestPhaseMapping(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{"Pending", "provisioning"},
		{"Degraded", "provisioning"},
		{"Ready", "provisioned"},
		{"Terminating", "decommissioning"},
	}
	for _, tc := range cases {
		if got := phaseToState(tc.phase); got != tc.want {
			t.Errorf("phaseToState(%q) = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestAssetPhaseMapping(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{"Running", "provisioned"},
		{"Succeeded", "provisioned"},
		{"Pending", "provisioning"},
		{"Terminating", "decommissioning"},
		{"Unknown", "pending"},
		{"", "pending"},
	}
	for _, tc := range cases {
		if got := assetPhaseToState(tc.phase); got != tc.want {
			t.Errorf("assetPhaseToState(%q) = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestReconcileLabEnvReady(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	expiresAt := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	obj := mustUnstructured(map[string]any{
		"apiVersion": "lab.rootenv.io/v1alpha1",
		"kind":       "LabEnvironment",
		"metadata": map[string]any{
			"name":            "abc123",
			"resourceVersion": "42",
		},
		"status": map[string]any{
			"phase":     "Ready",
			"expiresAt": expiresAt.Format(time.RFC3339),
			"assets": []any{
				map[string]any{
					"name":      "workstation",
					"phase":     "Running",
					"ready":     true,
					"protocols": []any{"ssh"},
				},
			},
		},
	})

	r.ReconcileLabEnv(context.Background(), obj)

	if w.id != "abc123" {
		t.Fatalf("id = %q, want abc123", w.id)
	}
	if w.patch["current_state"] != "provisioned" {
		t.Errorf("current_state = %v", w.patch["current_state"])
	}
	// expires_at should be present on first reconcile
	if _, ok := w.patch["expires_at"]; !ok {
		t.Error("expires_at missing from first patch")
	}
	// assets should be serialised JSON
	assetsJSON, ok := w.patch["assets"]
	if !ok {
		t.Fatal("assets missing from patch")
	}
	var assets []map[string]any
	b, _ := json.Marshal(assetsJSON)
	_ = json.Unmarshal(b, &assets)
	if len(assets) != 1 {
		t.Fatalf("len(assets) = %d", len(assets))
	}
	if assets[0]["name"] != "workstation" {
		t.Errorf("assets[0].name = %v", assets[0]["name"])
	}
	if assets[0]["state"] != "provisioned" {
		t.Errorf("assets[0].state = %v", assets[0]["state"])
	}
	if assets[0]["status"] != "poweredon" {
		t.Errorf("assets[0].status = %v", assets[0]["status"])
	}
	protos, _ := assets[0]["protocols"].([]any)
	if len(protos) != 1 || protos[0] != "ssh" {
		t.Errorf("assets[0].protocols = %v", assets[0]["protocols"])
	}
}

func TestReconcileLabEnvSkipsDuplicateResourceVersion(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{
			"name":            "abc123",
			"resourceVersion": "42",
		},
		"status": map[string]any{"phase": "Ready"},
	})

	r.ReconcileLabEnv(context.Background(), obj)
	w.id = ""   // reset
	w.patch = nil

	r.ReconcileLabEnv(context.Background(), obj) // same resourceVersion
	if w.id != "" {
		t.Error("expected second call to be skipped, but PatchAttempt was called")
	}
}

func TestReconcileLabEnvSkipsExpiresAtAfterFirstWrite(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "1"},
		"status": map[string]any{
			"phase":     "Ready",
			"expiresAt": "2026-06-16T12:00:00Z",
			"assets":    []any{},
		},
	})
	r.ReconcileLabEnv(context.Background(), obj)
	if _, ok := w.patch["expires_at"]; !ok {
		t.Fatal("expires_at missing on first write")
	}

	// second reconcile with new resourceVersion
	obj.SetResourceVersion("2")
	w.patch = nil
	r.ReconcileLabEnv(context.Background(), obj)
	if _, ok := w.patch["expires_at"]; ok {
		t.Error("expires_at should not be written a second time")
	}
}

func TestReconcileDeleteSetsDecommissioned(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "5"},
	})

	r.ReconcileDelete(context.Background(), obj)

	if w.id != "abc123" {
		t.Fatalf("id = %q, want abc123", w.id)
	}
	if w.patch["current_state"] != "decommissioned" {
		t.Errorf("current_state = %v", w.patch["current_state"])
	}
}

func TestReconcileLabEnvEmptyPhaseSkips(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "1"},
		"status":   map[string]any{"phase": ""},
	})
	r.ReconcileLabEnv(context.Background(), obj)
	if w.id != "" {
		t.Error("expected empty phase to skip PATCH, but PatchAttempt was called")
	}
}

func TestReconcileLabEnvSetsExpiresAtFromMetav1Time(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	ts := metav1.NewTime(time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC))
	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "1"},
		"status": map[string]any{
			"phase":     "Ready",
			"expiresAt": ts.UTC().Format(time.RFC3339),
			"assets":    []any{},
		},
	})
	r.ReconcileLabEnv(context.Background(), obj)
	if w.patch["expires_at"] != "2026-06-16 12:00:00.000Z" {
		t.Errorf("expires_at = %v", w.patch["expires_at"])
	}
}
