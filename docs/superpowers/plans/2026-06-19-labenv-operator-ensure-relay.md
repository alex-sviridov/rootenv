# labenv-operator ensureRelay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ensureRelay` to the labenv-operator controller so that every `LabEnvironment` gets a `relay-primitive` Deployment (plus ServiceAccount, Role, RoleBinding, Service, Ingress, NetworkPolicy) in its lab namespace.

**Architecture:** A new file `relay.go` in the `controller` package contains `ensureRelay` and one sub-function per Kubernetes resource. All sub-functions follow the existing create-once pattern: Get → if NotFound, Create → IgnoreAlreadyExists. The main controller calls `ensureRelay` in `reconcileCreate` after `ensureLimitRange`.

**Tech Stack:** Go 1.25, controller-runtime v0.23.3, k8s.io/api v0.35, Ginkgo/Gomega for tests, envtest for integration tests.

## Global Constraints

- Module path: `github.com/alex-sviridov/rootenv/services/labenv-operator`
- All work is inside `services/labenv-operator/`
- Create-once pattern only — no update/patch on reconcile
- `RELAY_EXEC_IMAGE` env var is required; return error from `ensureRelay` if missing
- `RELAY_NAMESPACE` env var in the Deployment is set to `nsName` (the lab namespace)
- No commits until explicitly requested by the user

---

### Task 1: Create `relay.go` with `ensureRelay` and all sub-functions

**Files:**
- Create: `services/labenv-operator/internal/controller/relay.go`
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller.go` (add call site only)

**Interfaces:**
- Produces: `func (r *LabEnvironmentReconciler) ensureRelay(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error`

- [ ] **Step 1: Create `relay.go`**

Create `services/labenv-operator/internal/controller/relay.go` with the following complete content:

```go
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

	labv1alpha1 "github.com/alex-sviridov/rootenv/services/labenv-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;bind;escalate
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create

type relayConfig struct {
	image           string
	ingressClass    string
	ingressBasePath string
	ingressAnnotations map[string]string
}

func loadRelayConfig() (relayConfig, error) {
	image := os.Getenv("RELAY_EXEC_IMAGE")
	if image == "" {
		return relayConfig{}, fmt.Errorf("RELAY_EXEC_IMAGE env var is required")
	}

	basePath := os.Getenv("RELAY_INGRESS_BASE_PATH")
	if basePath == "" {
		basePath = "/relay"
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
	if err := r.ensureRelayDeployment(ctx, nsName, cfg); err != nil {
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

func (r *LabEnvironmentReconciler) ensureRelayDeployment(ctx context.Context, nsName string, cfg relayConfig) error {
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-primitive"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-primitive",
			Namespace: nsName,
			Labels:    map[string]string{"app": "relay-primitive"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-primitive"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "relay-primitive"},
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
							Name:            "relay-primitive",
							Image:           cfg.image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "RELAY_NAMESPACE", Value: nsName},
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
										Path: "/",
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
			Selector: map[string]string{"app": "relay-primitive"},
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
			Name:        "relay",
			Namespace:   nsName,
			Annotations: cfg.ingressAnnotations,
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
	if len(cfg.ingressAnnotations) == 0 {
		ingress.Annotations = nil
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &ingress))
}

func (r *LabEnvironmentReconciler) ensureRelayNetworkPolicy(ctx context.Context, nsName string) error {
	var existing networkingv1.NetworkPolicy
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "allow-traefik-to-relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	tcp := corev1.ProtocolTCP
	port := intstr.FromInt32(8080)
	np := networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-traefik-to-relay",
			Namespace: nsName,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-primitive"},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &port},
					},
				},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &np))
}
```

- [ ] **Step 2: Add the `ensureRelay` call in `reconcileCreate`**

In `services/labenv-operator/internal/controller/labenvironment_controller.go`, find the block after `ensureLimitRange` and before the assets loop:

```go
	if err := r.ensureLimitRange(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}
	// get asset status
```

Replace with:

```go
	if err := r.ensureLimitRange(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureRelay(ctx, env, nsName); err != nil {
		return ctrl.Result{}, err
	}
	// get asset status
```

- [ ] **Step 3: Verify it compiles**

```bash
cd services/labenv-operator && go build ./...
```

Expected: no output (clean build).

- [ ] **Step 4: Add tests for `ensureRelay` in the existing test file**

The test suite uses envtest (a real API server). Add a new `Describe` block to `services/labenv-operator/internal/controller/labenvironment_controller_test.go`.

Add these imports if not already present:
```go
import (
    appsv1 "k8s.io/api/apps/v1"
    networkingv1 "k8s.io/api/networking/v1"
    rbacv1 "k8s.io/api/rbac/v1"
    corev1 "k8s.io/api/core/v1"
)
```

Add this block at the end of the file, inside the `package controller` scope but outside the existing `Describe`:

```go
var _ = Describe("ensureRelay", func() {
	const envName = "relay-test-env"
	const nsName = "rootenv-lab-" + envName
	ctx := context.Background()

	BeforeEach(func() {
		t.Setenv("RELAY_EXEC_IMAGE", "relay-primitive:test")
		t.Setenv("RELAY_INGRESS_CLASS", "traefik")
		t.Setenv("RELAY_INGRESS_BASE_PATH", "/relay")
		t.Setenv("RELAY_INGRESS_ANNOTATIONS", "traefik.ingress.kubernetes.io/router.entrypoints=websecure")

		By("creating the lab namespace")
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))
	})

	AfterEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		_ = k8sClient.Delete(ctx, ns)
	})

	It("creates all relay resources", func() {
		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: labv1alpha1.LabEnvironmentSpec{
				OwnerId: "usr-test",
				LabId:   "test-lab",
				Assets:  []labv1alpha1.Asset{{Name: "main", Image: "busybox"}},
			},
		}

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelay(ctx, env, nsName)).To(Succeed())

		By("ServiceAccount relay exists")
		var sa corev1.ServiceAccount
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &sa)).To(Succeed())

		By("Role relay exists with correct rules")
		var role rbacv1.Role
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &role)).To(Succeed())
		Expect(role.Rules).To(HaveLen(2))

		By("RoleBinding relay exists")
		var rb rbacv1.RoleBinding
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &rb)).To(Succeed())
		Expect(rb.Subjects[0].Name).To(Equal("relay"))

		By("Deployment relay-primitive exists with correct env")
		var deploy appsv1.Deployment
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-primitive"}, &deploy)).To(Succeed())
		Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("relay-primitive:test"))
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RELAY_NAMESPACE", Value: nsName},
		))

		By("Service relay exists")
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &svc)).To(Succeed())
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))

		By("Ingress relay exists with correct path and annotation")
		var ing networkingv1.Ingress
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
		Expect(ing.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/relay/" + envName))
		Expect(ing.Annotations).To(HaveKey("traefik.ingress.kubernetes.io/router.entrypoints"))
		Expect(*ing.Spec.IngressClassName).To(Equal("traefik"))

		By("NetworkPolicy allow-traefik-to-relay exists")
		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "allow-traefik-to-relay"}, &np)).To(Succeed())
		Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "relay-primitive"))

		By("ensureRelay is idempotent — second call does not error")
		Expect(r.ensureRelay(ctx, env, nsName)).To(Succeed())
	})

	It("returns error when RELAY_EXEC_IMAGE is missing", func() {
		t.Setenv("RELAY_EXEC_IMAGE", "")
		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec:       labv1alpha1.LabEnvironmentSpec{OwnerId: "usr-test", LabId: "lab", Assets: []labv1alpha1.Asset{{Name: "m", Image: "b"}}},
		}
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelay(ctx, env, nsName)).To(MatchError(ContainSubstring("RELAY_EXEC_IMAGE")))
	})
})
```

**Note:** Ginkgo `It` blocks don't have access to `testing.T`. Replace `t.Setenv` with `os.Setenv` + deferred `os.Unsetenv`, or use `DeferCleanup`. Rewrite the env setup as:

```go
BeforeEach(func() {
    DeferCleanup(func() {
        os.Unsetenv("RELAY_EXEC_IMAGE")
        os.Unsetenv("RELAY_INGRESS_CLASS")
        os.Unsetenv("RELAY_INGRESS_BASE_PATH")
        os.Unsetenv("RELAY_INGRESS_ANNOTATIONS")
    })
    os.Setenv("RELAY_EXEC_IMAGE", "relay-primitive:test")
    os.Setenv("RELAY_INGRESS_CLASS", "traefik")
    os.Setenv("RELAY_INGRESS_BASE_PATH", "/relay")
    os.Setenv("RELAY_INGRESS_ANNOTATIONS", "traefik.ingress.kubernetes.io/router.entrypoints=websecure")
    // ...
})
```

And in the "returns error" test:
```go
It("returns error when RELAY_EXEC_IMAGE is missing", func() {
    os.Unsetenv("RELAY_EXEC_IMAGE")
    // ...
})
```

- [ ] **Step 5: Run tests**

```bash
cd services/labenv-operator && go test ./internal/controller/... -v 2>&1 | tail -30
```

Expected: all tests pass, including the new `ensureRelay` describe block.

- [ ] **Step 6: Verify RBAC markers generate correctly**

```bash
cd services/labenv-operator && make manifests 2>&1
```

Expected: exits 0. Check that `config/rbac/role.yaml` now contains entries for `apps/deployments`, `rbac.authorization.k8s.io/roles`, `rbac.authorization.k8s.io/rolebindings`, `networking.k8s.io/ingresses`, and `serviceaccounts`.

```bash
grep -E "deployments|rolebindings|ingresses" services/labenv-operator/config/rbac/role.yaml
```

Expected: lines for each of those resources.
