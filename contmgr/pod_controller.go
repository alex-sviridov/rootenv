package main

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type PodStatusController struct {
	client.Client
	contmgr *Contmgr
}

func (c *PodStatusController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	var pod corev1.Pod
	if err := c.Get(ctx, req.NamespacedName, &pod); err != nil {
		if k8serrors.IsNotFound(err) {
			attemptID := namespaceToAttemptID(req.Namespace)
			if attemptID == "" {
				return reconcile.Result{}, nil
			}
			// Pod name equals asset name (see names.go: podName(assetName) = assetName).
			// Phase "" signals deletion → "stopped".
			return reconcile.Result{}, c.contmgr.UpdateAssetStatusFromPod(ctx, req.Name, attemptID, "")
		}
		return reconcile.Result{}, err
	}

	assetName := pod.Labels["rootenv.io/asset-name"]
	attemptID := pod.Labels["rootenv.io/attempt-id"]
	if assetName == "" || attemptID == "" {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, c.contmgr.UpdateAssetStatusFromPod(ctx, assetName, attemptID, string(pod.Status.Phase))
}

func (c *PodStatusController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("pod-status").
		For(&corev1.Pod{}).
		Complete(c)
}

// namespaceToAttemptID extracts the attempt ID from a managed namespace name.
// Returns "" for namespaces not matching the "rootenv-lab-{id}" pattern.
func namespaceToAttemptID(ns string) string {
	const prefix = "rootenv-lab-"
	if !strings.HasPrefix(ns, prefix) || len(ns) == len(prefix) {
		return ""
	}
	return ns[len(prefix):]
}
