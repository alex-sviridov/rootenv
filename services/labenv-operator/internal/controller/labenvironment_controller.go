package controller

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
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
	if err := r.ensureRelay(ctx, env, nsName); err != nil {
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
		if err := r.deleteIngressRoute(ctx, env.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleteIngressRoute: %w", err)
		}
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
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networkpolicy-denyall",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		tcp := corev1.ProtocolTCP
		dnsPort := intstr.FromInt32(53)
		udp := corev1.ProtocolUDP

		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // all pods in the namespace
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// allow incoming from the same namespace (for inter-pod communication)
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": nsName,
							},
						},
					}},
				},
				// allow incoming from Traefik (kube-system) to relay-exec on port 8080
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "kube-system",
							},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{{
						Protocol: protocolPtr(corev1.ProtocolTCP),
						Port:     portPtr(8080),
					}},
				},
			},
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
				// allow relay-exec pods to reach kube-apiserver on port 6443 (for kubectl exec)
				// empty To means: allow to all destinations on this port
				{
					Ports: []networkingv1.NetworkPolicyPort{{
						Protocol: protocolPtr(corev1.ProtocolTCP),
						Port:     portPtr(6443),
					}},
				},
			},
		}
		return nil
	})
	return err
}

func protocolPtr(p corev1.Protocol) *corev1.Protocol { return &p }
func portPtr(p int32) *intstr.IntOrString            { v := intstr.FromInt32(p); return &v }

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

func (r *LabEnvironmentReconciler) ensureRelayServiceAccount(ctx context.Context, nsName string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-exec-sa",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error { return nil })
	return err
}

func (r *LabEnvironmentReconciler) ensureRelayRole(ctx context.Context, nsName string) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-role", Namespace: nsName},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}},
			{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
		}
		return nil
	})
	return err
}

func (r *LabEnvironmentReconciler) ensureRelayRoleBinding(ctx context.Context, nsName string) error {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-rb", Namespace: nsName},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "relay-exec-role",
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: "relay-exec-sa", Namespace: nsName},
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "relay-exec-role",
		}
		rb.Subjects = []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: "relay-exec-sa", Namespace: nsName},
		}
		return nil
	})
	return err
}

func (r *LabEnvironmentReconciler) ensureRelayDeployment(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	image := os.Getenv("LABENV_RELAY_EXEC_IMAGE")
	if image == "" {
		return fmt.Errorf("LABENV_RELAY_EXEC_IMAGE env var not set")
	}
	labels := map[string]string{"app": "relay-exec"}
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "relay-exec", Namespace: nsName},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		dep.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName: "relay-exec-sa",
					Containers: []corev1.Container{{
						Name:  "relay-exec",
						Image: image,
						Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
						Env: []corev1.EnvVar{
							{Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
							{Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
							{Name: "RELAY_MY_NAMESPACE", Value: nsName},
							{Name: "RELAY_PORT", Value: "8080"},
						},
					}},
				},
			},
		}
		return nil
	})
	return err
}

func (r *LabEnvironmentReconciler) ensureRelayService(ctx context.Context, nsName string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-svc", Namespace: nsName},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{"app": "relay-exec"},
			Ports: []corev1.ServicePort{{
				Port:       8080,
				TargetPort: intstr.FromInt(8080),
				Protocol:   corev1.ProtocolTCP,
			}},
			Type: corev1.ServiceTypeClusterIP,
		}
		return nil
	})
	return err
}

const traefikInfraNamespace = "rootenv-infra"

var traefikGVK = map[string]schema.GroupVersionKind{
	"Middleware": {
		Group:   "traefik.io",
		Version: "v1alpha1",
		Kind:    "Middleware",
	},
	"IngressRoute": {
		Group:   "traefik.io",
		Version: "v1alpha1",
		Kind:    "IngressRoute",
	},
}

func (r *LabEnvironmentReconciler) ensureIngressRoute(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	attemptID := env.Name

	// Middleware 1: inject X-Attempt-Id header
	mwHeaders := &unstructured.Unstructured{}
	mwHeaders.SetGroupVersionKind(traefikGVK["Middleware"])
	mwHeaders.SetName("relay-exec-headers-" + attemptID)
	mwHeaders.SetNamespace(traefikInfraNamespace)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, mwHeaders, func() error {
		mwHeaders.Object["spec"] = map[string]interface{}{
			"headers": map[string]interface{}{
				"customRequestHeaders": map[string]interface{}{
					"X-Attempt-Id": attemptID,
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ensure Middleware headers: %w", err)
	}

	// Middleware 2: ForwardAuth
	mwAuth := &unstructured.Unstructured{}
	mwAuth.SetGroupVersionKind(traefikGVK["Middleware"])
	mwAuth.SetName("relay-exec-auth-" + attemptID)
	mwAuth.SetNamespace(traefikInfraNamespace)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, mwAuth, func() error {
		mwAuth.Object["spec"] = map[string]interface{}{
			"forwardAuth": map[string]interface{}{
				"address": "http://ingress-authenticator-svc.rootenv-infra.svc/auth",
				"authRequestHeaders": []interface{}{
					"Authorization",
					"X-Attempt-Id",
				},
				"authResponseHeaders": []interface{}{
					"X-User-Id",
					"X-Attempt-Id",
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ensure Middleware auth: %w", err)
	}

	// Middleware 3: stripPrefix
	mwStrip := &unstructured.Unstructured{}
	mwStrip.SetGroupVersionKind(traefikGVK["Middleware"])
	mwStrip.SetName("relay-exec-strip-" + attemptID)
	mwStrip.SetNamespace(traefikInfraNamespace)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, mwStrip, func() error {
		mwStrip.Object["spec"] = map[string]interface{}{
			"stripPrefix": map[string]interface{}{
				"prefixes": []interface{}{
					"/relay/" + attemptID + "/exec",
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ensure Middleware strip: %w", err)
	}

	// IngressRoute
	ir := &unstructured.Unstructured{}
	ir.SetGroupVersionKind(traefikGVK["IngressRoute"])
	ir.SetName("relay-exec-" + attemptID)
	ir.SetNamespace(traefikInfraNamespace)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, ir, func() error {
		labels := ir.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels["rootenv.io/attempt-id"] = attemptID
		ir.SetLabels(labels)
		ir.Object["spec"] = map[string]interface{}{
			"entryPoints": []interface{}{"websecure"},
			"routes": []interface{}{
				map[string]interface{}{
					"match": "PathPrefix(`/relay/" + attemptID + "/exec/`)",
					"kind":  "Rule",
					"middlewares": []interface{}{
						map[string]interface{}{
							"name":      "relay-exec-headers-" + attemptID,
							"namespace": traefikInfraNamespace,
						},
						map[string]interface{}{
							"name":      "relay-exec-auth-" + attemptID,
							"namespace": traefikInfraNamespace,
						},
						map[string]interface{}{
							"name":      "relay-exec-strip-" + attemptID,
							"namespace": traefikInfraNamespace,
						},
					},
					"services": []interface{}{
						map[string]interface{}{
							"name":      "relay-exec-svc",
							"namespace": nsName,
							"port":      int64(8080),
						},
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ensure IngressRoute: %w", err)
	}

	return nil
}

func (r *LabEnvironmentReconciler) deleteIngressRoute(ctx context.Context, attemptID string) error {
	toDelete := []struct{ kind, name string }{
		{"Middleware", "relay-exec-headers-" + attemptID},
		{"Middleware", "relay-exec-auth-" + attemptID},
		{"Middleware", "relay-exec-strip-" + attemptID},
		{"IngressRoute", "relay-exec-" + attemptID},
	}
	for _, td := range toDelete {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(traefikGVK[td.kind])
		obj.SetName(td.name)
		obj.SetNamespace(traefikInfraNamespace)
		if err := r.Client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete %s %s: %w", td.kind, td.name, err)
		}
	}
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
