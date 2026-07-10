package downstream

import (
	"context"
	"log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
)

const DesiredStateDecommissioned = "decommissioned"

// PocketBaseClient is the subset of the PocketBase HTTP client that the
// Reconciler needs. Keeping it narrow here means upstream sync can add its own
// interface without touching this one.
type PocketBaseClient interface {
	ListActiveAttempts(ctx context.Context) ([]Attempt, error)
	PatchAttempt(ctx context.Context, id string, patch map[string]any) error
}

// Attempt is the controller's internal domain type. It is populated by the
// pocketbase package (which maps AttemptRecord → Attempt) and carries
// controller-set fields (DecommissionReason) without json tags.
type Attempt struct {
	ID                 string
	UserID             string
	UserName           string
	LabID              string
	DesiredState       string
	DecommissionReason string
	Environment        EnvironmentSpec
	Exercises          []Exercise
}

type EnvironmentSpec struct {
	Duration int     `json:"duration"`
	Assets   []Asset `json:"assets"`
}

type Asset struct {
	Name           string   `json:"name"`
	Image          string   `json:"image"`
	CPU            string   `json:"cpu"`
	Memory         string   `json:"memory"`
	Disk           string   `json:"disk"`
	Setup          string   `json:"setup"`
	RelayProtocols []string `json:"protocols"`
}

// Exercise is a gradeable item embedded in a lab's task markdown, extracted
// by scripts/labs_sync_exercises.py at sync time.
type Exercise struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Asset       string `json:"asset,omitempty"`
	Template    string `json:"template"`
}

// Reconciler applies attempt state to Kubernetes LabEnvironment resources.
type Reconciler struct {
	dyn dynamic.Interface
	pb  PocketBaseClient
}

func NewReconciler(dyn dynamic.Interface, pb PocketBaseClient) *Reconciler {
	return &Reconciler{dyn: dyn, pb: pb}
}

// toLabEnvironment translates an Attempt into a LabEnvironment custom resource
// (lab.rootenv.io/v1alpha1). Field names must be kept in sync with
// LabEnvironmentSpec/Asset/Exercise in services/labenv-operator/api/v1alpha1/labenvironment_types.go.
func (r *Reconciler) toLabEnvironment(a Attempt) *unstructured.Unstructured {
	assets := make([]any, 0, len(a.Environment.Assets))
	for _, assetItem := range a.Environment.Assets {
		protocols := assetItem.RelayProtocols
		if protocols == nil {
			protocols = []string{}
		}
		assets = append(assets, map[string]any{
			"name":      assetItem.Name,
			"image":     assetItem.Image,
			"cpu":       assetItem.CPU,
			"memory":    assetItem.Memory,
			"disk":      assetItem.Disk,
			"setup":     assetItem.Setup,
			"protocols": protocols,
		})
	}
	exercises := make([]any, 0, len(a.Exercises))
	for _, ex := range a.Exercises {
		exercises = append(exercises, map[string]any{
			"id":          ex.ID,
			"description": ex.Description,
			"type":        ex.Type,
			"asset":       ex.Asset,
			"template":    ex.Template,
		})
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "lab.rootenv.io/v1alpha1",
			"kind":       "LabEnvironment",
			"metadata": map[string]any{
				"name": a.ID,
			},
			"spec": map[string]any{
				"ownerId":   a.UserID,
				"ownerName": a.UserName,
				"labId":     a.LabID,
				"ttl":       a.Environment.Duration,
				"assets":    assets,
				"exercises": exercises,
			},
		},
	}
}

func (r *Reconciler) ReconcileAttempt(ctx context.Context, a Attempt) {
	if a.DesiredState == DesiredStateDecommissioned {
		log.Printf("attempt %s: decommission requested (reason: %s), deleting LabEnvironment", a.ID, a.DecommissionReason)
		err := r.dyn.Resource(k8s.LabEnvironmentGVR).Delete(ctx, a.ID, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			// LabEnvironment never existed or was already deleted — upstream
			// won't fire a Delete event, so patch PocketBase directly.
			if patchErr := r.pb.PatchAttempt(ctx, a.ID, map[string]any{"current_state": DesiredStateDecommissioned}); patchErr != nil {
				log.Printf("attempt %s: failed to mark decommissioned: %v", a.ID, patchErr)
			} else {
				log.Printf("attempt %s: LabEnvironment not found, marked decommissioned", a.ID)
			}
		} else if err != nil {
			log.Printf("attempt %s: failed to delete LabEnvironment: %v", a.ID, err)
		}
		return
	}

	obj := r.toLabEnvironment(a)
	_, err := r.dyn.Resource(k8s.LabEnvironmentGVR).Apply(ctx, a.ID, obj, metav1.ApplyOptions{
		FieldManager: "attempt-controller",
		Force:        true,
	})
	if err != nil {
		log.Printf("attempt %s: failed to apply LabEnvironment: %v", a.ID, err)
		return
	}
	log.Printf("attempt %s: applied LabEnvironment", a.ID)
}

func (r *Reconciler) ResyncAttempts(ctx context.Context) {
	attempts, err := r.pb.ListActiveAttempts(ctx)
	if err != nil {
		log.Printf("resync: list attempts failed: %v", err)
		return
	}
	for _, a := range attempts {
		r.ReconcileAttempt(ctx, a)
	}
}
