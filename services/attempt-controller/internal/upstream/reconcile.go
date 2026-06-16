package upstream

import (
	"context"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PocketBaseWriter is the subset of pocketbase.Client the upstream needs.
type PocketBaseWriter interface {
	PatchAttempt(ctx context.Context, id string, patch map[string]any) error
}

// Reconciler syncs LabEnvironment status back to PocketBase attempts.
// It is not safe for concurrent use — Run() must be the only caller.
type Reconciler struct {
	pb               PocketBaseWriter
	lastRV           map[string]string // attemptID → last synced resourceVersion
	expiresAtWritten map[string]bool   // attemptIDs for which expires_at has been written
}

func NewReconciler(pb PocketBaseWriter) *Reconciler {
	return &Reconciler{
		pb:               pb,
		lastRV:           make(map[string]string),
		expiresAtWritten: make(map[string]bool),
	}
}

// ReconcileLabEnv handles an Add or Update event for a LabEnvironment.
func (r *Reconciler) ReconcileLabEnv(ctx context.Context, obj *unstructured.Unstructured) {
	id := obj.GetName()
	rv := obj.GetResourceVersion()

	if r.lastRV[id] == rv {
		return
	}

	status, _, _ := unstructured.NestedMap(obj.Object, "status")
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase == "" {
		return // status not yet set by operator
	}

	patch := map[string]any{
		"current_state": phaseToState(phase),
		"assets":        r.buildAssets(status),
	}

	if !r.expiresAtWritten[id] {
		expiresAt, _, _ := unstructured.NestedString(obj.Object, "status", "expiresAt")
		if expiresAt != "" {
			t, err := time.Parse(time.RFC3339, expiresAt)
			if err == nil {
				// PocketBase date format: "2006-01-02 15:04:05.000Z"
				patch["expires_at"] = t.UTC().Format("2006-01-02 15:04:05.000Z")
			}
		}
	}

	if err := r.pb.PatchAttempt(ctx, id, patch); err != nil {
		log.Printf("upstream: attempt %s: patch failed: %v", id, err)
		return
	}

	r.lastRV[id] = rv
	if _, ok := patch["expires_at"]; ok {
		r.expiresAtWritten[id] = true
	}
	log.Printf("upstream: attempt %s: synced phase=%s", id, phase)
}

// ReconcileDelete handles a Delete event — sets current_state to decommissioned.
func (r *Reconciler) ReconcileDelete(ctx context.Context, obj *unstructured.Unstructured) {
	id := obj.GetName()
	patch := map[string]any{"current_state": "decommissioned"}
	if err := r.pb.PatchAttempt(ctx, id, patch); err != nil {
		log.Printf("upstream: attempt %s: delete patch failed: %v", id, err)
		return
	}
	delete(r.lastRV, id)
	delete(r.expiresAtWritten, id)
	log.Printf("upstream: attempt %s: marked decommissioned", id)
}

// phaseToState maps LabEnvironment.Status.Phase to attempts.current_state.
func phaseToState(phase string) string {
	switch phase {
	case "Ready":
		return "provisioned"
	case "Terminating":
		return "decommissioning"
	default: // Pending, Degraded, or anything unexpected
		return "provisioning"
	}
}

// assetPhaseToState maps AssetStatus.Phase to the assets.state select value.
func assetPhaseToState(phase string) string {
	switch phase {
	case "Running", "Succeeded":
		return "provisioned"
	case "Pending":
		return "provisioning"
	case "Terminating":
		return "decommissioning"
	default:
		return "pending"
	}
}

// buildAssets converts status.assets into the JSON value for attempts.assets.
func (r *Reconciler) buildAssets(status map[string]any) []map[string]any {
	raw, _ := status["assets"].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		a, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := a["name"].(string)
		phase, _ := a["phase"].(string)

		protocols := []string{}
		if rawProtos, ok := a["protocols"].([]any); ok {
			for _, p := range rawProtos {
				if s, ok := p.(string); ok {
					protocols = append(protocols, s)
				}
			}
		}

		result = append(result, map[string]any{
			"name":      name,
			"state":     assetPhaseToState(phase),
			"status":    "poweredon",
			"protocols": protocols,
		})
	}
	return result
}
