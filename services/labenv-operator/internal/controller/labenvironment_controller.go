package controller

import (
	"context"
	"os"

	"fmt"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"strings"
	"time"

	labv1alpha1 "github.com/alex-sviridov/rootenv/services/labenv-operator/api/v1alpha1"
)

const finalizerName = "labenv.rootenv.io/cleanup"

// LabEnvironmentReconciler reconciles a LabEnvironment object
type LabEnvironmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=lab.rootenv.io,resources=labenvironments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=lab.rootenv.io,resources=labenvironments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=lab.rootenv.io,resources=labenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=limitranges,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;delete

func (r *LabEnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var env labv1alpha1.LabEnvironment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nsName := "rootenv-lab-" + env.Name

	if !env.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &env, nsName)
	}
	return r.reconcileCreate(ctx, &env, nsName)
}

// reconcileCreate — provision namespace, pods, filnalizer.
func (r *LabEnvironmentReconciler) reconcileCreate(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if err := r.ensureFinalizer(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureNamespace(ctx, env, nsName); err != nil {
		return ctrl.Result{}, err
	}
	if env.Status.Namespace == "" {
		env.Status.Namespace = nsName
	}
	// expiration mechanism
	if env.Status.ExpiresAt == nil {
		env.Status.ExpiresAt = &metav1.Time{Time: env.CreationTimestamp.Add(time.Duration(env.Spec.TTL) * time.Minute)}
	}
	now := time.Now()
	if now.After(env.Status.ExpiresAt.Time) {
		log.Info("lab environment expired, deleting", "name", env.Name)
		if err := r.Delete(ctx, env); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, nil
	}

	if err := r.ensureNetworkPolicy(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureResourceQuota(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureLimitRange(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}
	// get asset status
	notReadyMsg := []string{}
	env.Status.Assets = nil
	for _, asset := range env.Spec.Assets {
		if err := r.ensurePod(ctx, env, nsName, asset); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.ensureHeadlessService(ctx, env, nsName, asset.Name); err != nil {
			return ctrl.Result{}, err
		}
		// get pod status
		phase := "Pending"
		reason := ""
		var pod corev1.Pod
		if err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: asset.Name}, &pod); err == nil {
			phase = string(pod.Status.Phase)
			reason = containerReason(&pod)
			if !pod.DeletionTimestamp.IsZero() {
				phase = "Terminating"
			}
		}
		ready := isPodReady(&pod)
		// add asset to status
		env.Status.Assets = append(env.Status.Assets, labv1alpha1.AssetStatus{
			Name:      asset.Name,
			Phase:     phase,
			Reason:    reason,
			Ready:     ready,
			Protocols: asset.Protocols,
			Address:   asset.Name + "." + nsName + ".svc.cluster.local",
		})

		if !ready {
			notReadyMsg = append(notReadyMsg, asset.Name+" - "+reason)
		}
	}

	// calculate overall status condition
	totalAssets := len(env.Spec.Assets)
	readyAssets := 0
	for _, a := range env.Status.Assets {
		if a.Ready {
			readyAssets++
		}
	}
	env.Status.TotalAssets = totalAssets
	env.Status.ReadyAssets = readyAssets
	env.Status.Ready = fmt.Sprintf("%d/%d", readyAssets, totalAssets)
	switch {
	case readyAssets == totalAssets:
		env.Status.Phase = "Ready"
	case readyAssets == 0:
		env.Status.Phase = "Pending"
	default:
		env.Status.Phase = "Degraded"
	}
	if readyAssets == totalAssets {
		meta.SetStatusCondition(&env.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "AllAssetsReady",
			Message: "all assets are ready",
		})
	} else {
		meta.SetStatusCondition(&env.Status.Conditions, metav1.Condition{
			Type:   "Ready",
			Status: metav1.ConditionFalse,
			Reason: "AssetsNotReady",
			Message: fmt.Sprintf("%d/%d assets ready; waiting for: %s",
				readyAssets, totalAssets, strings.Join(notReadyMsg, ", ")),
		})
	}

	if err := r.Status().Update(ctx, env); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	// schedule the timeout deletion
	remaining := time.Until(env.Status.ExpiresAt.Time)
	if remaining < 0 {
		remaining = time.Second // time is out, check right now
	}
	return ctrl.Result{RequeueAfter: remaining}, nil
}

func containerReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			return cs.State.Waiting.Reason
		}
		if cs.State.Terminated != nil {
			return cs.State.Terminated.Reason
		}
	}
	return ""
}

// isPodReady - checks pod readiness using readiness probe
func isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// reconcileDelete — decommission namespace and remove finalizer.
func (r *LabEnvironmentReconciler) reconcileDelete(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(env, finalizerName) {
		return ctrl.Result{}, nil
	}

	var ns corev1.Namespace
	err := r.Get(ctx, client.ObjectKey{Name: nsName}, &ns)

	if apierrors.IsNotFound(err) {
		controllerutil.RemoveFinalizer(env, finalizerName)
		if err := r.Update(ctx, env); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("cleanup complete, finalizer removed")
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if ns.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, &ns); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("requested namespace deletion", "namespace", nsName)
	}

	if env.Status.Phase != "Terminating" {
		env.Status.Phase = "Terminating"
		if err := r.Status().Update(ctx, env); err != nil {
			log.Error(err, "failed to set Terminating phase")
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *LabEnvironmentReconciler) ensureFinalizer(ctx context.Context, env *labv1alpha1.LabEnvironment) error {
	if controllerutil.ContainsFinalizer(env, finalizerName) {
		return nil
	}
	controllerutil.AddFinalizer(env, finalizerName)
	return r.Update(ctx, env)
}

func (r *LabEnvironmentReconciler) ensureNamespace(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	log := logf.FromContext(ctx)

	var ns corev1.Namespace
	err := r.Get(ctx, client.ObjectKey{Name: nsName}, &ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				"rootenv.io/attempt":    env.Name,
				"rootenv.io/owner-id":   env.Spec.OwnerId,
				"rootenv.io/lab-id":     env.Spec.LabId,
				"rootenv.io/managed-by": "labenv-operator",
			},
			Annotations: map[string]string{
				"rootenv.io/owner-name": env.Spec.OwnerName,
			},
		},
	}
	if err := r.Create(ctx, &ns); err != nil {
		return client.IgnoreAlreadyExists(err)
	}
	log.Info("created namespace", "namespace", nsName)
	return nil
}

func (r *LabEnvironmentReconciler) ensureNetworkPolicy(ctx context.Context, nsName string) error {
	var existing networkingv1.NetworkPolicy
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "network-policy"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	tcp := corev1.ProtocolTCP
	dnsPort := intstr.FromInt32(53)
	udp := corev1.ProtocolUDP

	np := networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "network-policy",
			Namespace: nsName,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // all pods in the namespace
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			// allow incoming from the same namespace (for inter-pod communication)
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": nsName,
						},
					},
				}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// allow outgoing to the same namespace (for inter-pod communication)
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": nsName,
							},
						},
					}},
				},
				// allow to kube-dns in kube-system for DNS resolution
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &dnsPort},
						{Protocol: &tcp, Port: &dnsPort},
					},
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "kube-system",
							},
						},
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"k8s-app": "kube-dns"},
						},
					}},
				},
			},
		},
	}
	if err := r.Create(ctx, &np); err != nil {
		return client.IgnoreAlreadyExists(err)
	}
	return nil
}

// ensureResourceQuota caps total resource usage in the namespace, protecting the cluster from a broken lab definition.
func (r *LabEnvironmentReconciler) ensureResourceQuota(ctx context.Context, nsName string) error {
	var existing corev1.ResourceQuota
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "resource-quota"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	rq := corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "resource-quota",
			Namespace: nsName,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:       resource.MustParse("2"),
				corev1.ResourceLimitsMemory:    resource.MustParse("4Gi"),
				corev1.ResourceRequestsStorage: resource.MustParse("5Gi"),
				corev1.ResourcePods:            resource.MustParse("6"),
			},
		},
	}
	if err := r.Create(ctx, &rq); err != nil {
		return client.IgnoreAlreadyExists(err)
	}
	return nil
}

// ensureLimitRange sets sane default and maximum per-container resource limits for the namespace.
func (r *LabEnvironmentReconciler) ensureLimitRange(ctx context.Context, nsName string) error {
	var existing corev1.LimitRange
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "limit-range"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	lr := corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "limit-range",
			Namespace: nsName,
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		},
	}
	if err := r.Create(ctx, &lr); err != nil {
		return client.IgnoreAlreadyExists(err)
	}
	return nil
}

func (r *LabEnvironmentReconciler) ensurePod(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string, asset labv1alpha1.Asset) error {
	log := logf.FromContext(ctx)

	var existing corev1.Pod
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: asset.Name}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	image := asset.Image
	if prefix := os.Getenv("LABENV_REGISTRY"); prefix != "" {
		image = prefix + "/" + asset.Image
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      asset.Name,
			Namespace: nsName,
			Labels: map[string]string{
				"rootenv.io/attempt":    env.Name,
				"rootenv.io/owner-id":   env.Spec.OwnerId,
				"rootenv.io/lab-id":     env.Spec.LabId,
				"rootenv.io/asset":      asset.Name,
				"rootenv.io/managed-by": "labenv-operator",
			},
			Annotations: map[string]string{
				"rootenv.io/owner-name": env.Spec.OwnerName,
			},
		},
		Spec: corev1.PodSpec{
			// Never: pod is not restarted on failure; a crashed lab is recreated, not revived
			RestartPolicy: corev1.RestartPolicyNever,
			// hostUsers: false maps container root to an unprivileged UID on the host
			HostUsers:                    ptr.To(false),
			HostNetwork:                  false,
			HostPID:                      false,
			HostIPC:                      false,
			AutomountServiceAccountToken: ptr.To(false),
			// SecurityContext with SeccompProfile set to RuntimeDefault applies the default seccomp profile to all containers in the pod, providing a baseline level of security.
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "main",
					Image:   image,
					Command: []string{"sleep", "infinity"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:              resource.MustParse(asset.CPU),
							corev1.ResourceMemory:           resource.MustParse(asset.Memory),
							corev1.ResourceEphemeralStorage: resource.MustParse(asset.Disk),
						},
					},
				},
			},
		},
	}
	if err := r.Create(ctx, &pod); err != nil {
		return err
	}
	log.Info("created pod", "namespace", nsName, "pod", asset.Name, "image", image)
	return nil
}

// ensureHeadlessService creates a headless service for the given asset, allowing other pods of namespace to discover it via DNS.
func (r *LabEnvironmentReconciler) ensureHeadlessService(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName, assetName string) error {
	log := logf.FromContext(ctx)

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      assetName,
			Namespace: nsName,
			Labels: map[string]string{
				"rootenv.io/attempt":    env.Name,
				"rootenv.io/owner-id":   env.Spec.OwnerId,
				"rootenv.io/lab-id":     env.Spec.LabId,
				"rootenv.io/managed-by": "labenv-operator",
			},
			Annotations: map[string]string{
				"rootenv.io/owner-name": env.Spec.OwnerName,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"rootenv.io/asset": assetName},
		},
	}
	if err := r.Create(ctx, &svc); err != nil {
		return client.IgnoreAlreadyExists(err)
	}
	log.Info("created headless service", "namespace", nsName, "asset", assetName)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LabEnvironmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&labv1alpha1.LabEnvironment{}).
		Named("labenvironment").
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.podToLabEnv),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetLabels()["rootenv.io/managed-by"] == "labenv-operator"
			})),
		).
		Complete(r)
}

func (r *LabEnvironmentReconciler) podToLabEnv(ctx context.Context, obj client.Object) []ctrl.Request {
	envName := obj.GetLabels()["rootenv.io/attempt"]
	if envName == "" {
		return nil
	}
	return []ctrl.Request{{NamespacedName: client.ObjectKey{Name: envName}}}
}
