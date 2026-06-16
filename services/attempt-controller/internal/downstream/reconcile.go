package downstream

import (
	"context"
	"encoding/json"
	"log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/pocketbase"
)

const DesiredStateDecommissioned = "decommissioned"

type environmentSpec struct {
	Duration int     `json:"duration"`
	Assets   []asset `json:"assets"`
}

type asset struct {
	Name           string   `json:"name"`
	Image          string   `json:"image"`
	CPU            string   `json:"cpu"`
	Memory         string   `json:"memory"`
	Disk           string   `json:"disk"`
	Setup          string   `json:"setup"`
	RelayProtocols []string `json:"protocols"`
}

// toLabEnvironment translates an attempt and its parsed environment into a
// LabEnvironment custom resource (lab.rootenv.io/v1alpha1). Field names below
// must be kept in sync with LabEnvironmentSpec/Asset in
// services/labenv-operator/api/v1alpha1/labenvironment_types.go.
func toLabEnvironment(a pocketbase.AttemptRecord, env environmentSpec) *unstructured.Unstructured {
	assets := make([]any, 0, len(env.Assets))
	for _, asset := range env.Assets {
		protocols := asset.RelayProtocols
		if protocols == nil {
			protocols = []string{}
		}
		assets = append(assets, map[string]any{
			"name":      asset.Name,
			"image":     asset.Image,
			"cpu":       asset.CPU,
			"memory":    asset.Memory,
			"disk":      asset.Disk,
			"setup":     asset.Setup,
			"protocols": protocols,
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
				"ownerId":   a.UserId,
				"ownerName": a.UserName,
				"labId":     a.Lab,
				"ttl":       env.Duration,
				"assets":    assets,
			},
		},
	}
}

func ReconcileAttempt(ctx context.Context, dyn dynamic.Interface, a pocketbase.AttemptRecord) {
	if a.DesiredState == DesiredStateDecommissioned {
		log.Printf("attempt %s: decommission requested (reason: %s), deleting LabEnvironment", a.ID, a.DecommissionReason)
		err := dyn.Resource(k8s.LabEnvironmentGVR).Delete(ctx, a.ID, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			log.Printf("attempt %s: failed to delete LabEnvironment: %v", a.ID, err)
			return
		}
		log.Printf("attempt %s: LabEnvironment deleted", a.ID)
		return
	}

	var env environmentSpec
	if err := json.Unmarshal(a.Expand.Lab.Environment, &env); err != nil {
		log.Printf("attempt %s: failed to parse environment: %v", a.ID, err)
		return
	}

	obj := toLabEnvironment(a, env)
	_, err := dyn.Resource(k8s.LabEnvironmentGVR).Apply(ctx, a.ID, obj, metav1.ApplyOptions{
		FieldManager: "attempt-controller",
		Force:        true,
	})
	if err != nil {
		log.Printf("attempt %s: failed to apply LabEnvironment: %v", a.ID, err)
		return
	}
	log.Printf("attempt %s: applied LabEnvironment", a.ID)
}

// resyncAttempts fetches all active attempts (current_state != desired_state)
// and reconciles each one. It is called at startup, after every successful
// (re)connection of the realtime subscription, and on a periodic timer, so
// that the controller self-heals from missed events or failed reconciles.
func ResyncAttempts(ctx context.Context, pb *pocketbase.Client, dyn dynamic.Interface) {
	attempts, err := pb.ListActiveAttempts()
	if err != nil {
		log.Printf("resync: list attempts failed: %v", err)
		return
	}
	for _, a := range attempts {
		ReconcileAttempt(ctx, dyn, a)
	}
}
