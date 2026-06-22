package controller

import (
	"context"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	labv1alpha1 "github.com/alex-sviridov/rootenv/services/labenv-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;bind;escalate
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create

type relayConfig struct {
	image              string
	ingressClass       string
	ingressBasePath    string
	ingressAnnotations map[string]string
}

func loadRelayConfig() (relayConfig, error) {
	image := os.Getenv("RELAY_IMAGE")
	if image == "" {
		return relayConfig{}, fmt.Errorf("RELAY_IMAGE env var is required")
	}

	basePath := os.Getenv("RELAY_INGRESS_BASE_PATH")
	if basePath == "" {
		basePath = "/relay/exec"
	}

	annotations := map[string]string{}
	if raw := os.Getenv("RELAY_INGRESS_ANNOTATIONS"); raw != "" {
		for _, token := range strings.Split(raw, ",") {
			k, v, ok := strings.Cut(token, "=")
			if !ok || strings.TrimSpace(k) == "" {
				continue
			}
			annotations[strings.TrimSpace(k)] = v
		}
	}

	return relayConfig{
		image:              image,
		ingressClass:       os.Getenv("RELAY_INGRESS_CLASS"),
		ingressBasePath:    basePath,
		ingressAnnotations: annotations,
	}, nil
}

func (r *LabEnvironmentReconciler) ensureRelay(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	cfg, err := loadRelayConfig()
	if err != nil {
		return err
	}

	if err := r.ensureRelayServiceAccount(ctx, nsName); err != nil {
		return err
	}
	if err := r.ensureRelayRole(ctx, nsName); err != nil {
		return err
	}
	if err := r.ensureRelayRoleBinding(ctx, nsName); err != nil {
		return err
	}
	if err := r.ensureRelayDeployment(ctx, env, nsName, cfg); err != nil {
		return err
	}
	if err := r.ensureRelayService(ctx, nsName); err != nil {
		return err
	}
	if err := r.ensureRelayIngress(ctx, env, nsName, cfg); err != nil {
		return err
	}
	if err := r.ensureRelayNetworkPolicy(ctx, nsName); err != nil {
		return err
	}
	return nil
}

func (r *LabEnvironmentReconciler) ensureRelayServiceAccount(ctx context.Context, nsName string) error {
	var existing corev1.ServiceAccount
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	sa := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay",
			Namespace: nsName,
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &sa))
}

func (r *LabEnvironmentReconciler) ensureRelayRole(ctx context.Context, nsName string) error {
	var existing rbacv1.Role
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay",
			Namespace: nsName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
				Verbs:     []string{"create"},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &role))
}

func (r *LabEnvironmentReconciler) ensureRelayRoleBinding(ctx context.Context, nsName string) error {
	var existing rbacv1.RoleBinding
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	rb := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay",
			Namespace: nsName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "relay",
				Namespace: nsName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "relay",
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &rb))
}

func (r *LabEnvironmentReconciler) ensureRelayDeployment(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string, cfg relayConfig) error {
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-exec"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-exec",
			Namespace: nsName,
			Labels:    map[string]string{"app": "relay-exec"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-exec"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "relay-exec"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "relay",
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(int64(10001)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "relay-exec",
							Image:           cfg.image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "RELAY_MY_NAMESPACE", Value: nsName},
								{Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
								{Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
								{Name: "RELAY_SKIP_AUTH", Value: "true"},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt32(8080),
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &deploy))
}

func (r *LabEnvironmentReconciler) ensureRelayService(ctx context.Context, nsName string) error {
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay",
			Namespace: nsName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "relay-exec"},
			Ports: []corev1.ServicePort{
				{Port: 8080, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &svc))
}

func (r *LabEnvironmentReconciler) ensureRelayIngress(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string, cfg relayConfig) error {
	var existing networkingv1.Ingress
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	path := cfg.ingressBasePath + "/" + env.Name
	pathType := networkingv1.PathTypePrefix

	ingress := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay",
			Namespace: nsName,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     path,
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "relay",
											Port: networkingv1.ServiceBackendPort{Number: 8080},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if cfg.ingressClass != "" {
		ingress.Spec.IngressClassName = &cfg.ingressClass
	}
	if len(cfg.ingressAnnotations) > 0 {
		ingress.Annotations = cfg.ingressAnnotations
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &ingress))
}

// apiServerEndpoint returns the actual IP and port of the kube-apiserver by
// reading the "kubernetes" Endpoints in the default namespace. This is necessary
// because k3s (and flannel-based CNIs in general) evaluate network policy after
// DNAT, so the ClusterIP (10.x.x.1:443) never matches — only the post-DNAT
// node IP and real port (e.g. 172.18.0.2:6443) do.
func (r *LabEnvironmentReconciler) apiServerEndpoint(ctx context.Context) (ip string, port int32, err error) {
	var ep corev1.Endpoints
	if err = r.Get(ctx, client.ObjectKey{Namespace: "default", Name: "kubernetes"}, &ep); err != nil {
		return
	}
	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			for _, p := range subset.Ports {
				return addr.IP, p.Port, nil
			}
		}
	}
	err = fmt.Errorf("kubernetes endpoints has no addresses")
	return
}

func (r *LabEnvironmentReconciler) ensureRelayNetworkPolicy(ctx context.Context, nsName string) error {
	apiIP, apiPort, err := r.apiServerEndpoint(ctx)
	if err != nil {
		return fmt.Errorf("resolving apiserver endpoint: %w", err)
	}
	apiServerCIDR := apiIP + "/32"
	apiPortVal := intstr.FromInt32(apiPort)

	tcp := corev1.ProtocolTCP
	wsPort := intstr.FromInt32(8080)

	notRelayExec := metav1.LabelSelectorRequirement{
		Key:      "app",
		Operator: metav1.LabelSelectorOpNotIn,
		Values:   []string{"relay-exec"},
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networkpolicy-relay-exec",
			Namespace: nsName,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-exec"},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// allow WebSocket connections from ingress controller
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "kube-system",
							},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &wsPort},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// reach lab pods only (not relay-exec itself) for kubectl exec
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": nsName,
							},
						},
						PodSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{notRelayExec},
						},
					}},
				},
				// reach kube-apiserver for pods/exec API calls
				{
					To: []networkingv1.NetworkPolicyPeer{{
						IPBlock: &networkingv1.IPBlock{CIDR: apiServerCIDR},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &apiPortVal},
					},
				},
			},
		}
		return nil
	})
	return err
}
