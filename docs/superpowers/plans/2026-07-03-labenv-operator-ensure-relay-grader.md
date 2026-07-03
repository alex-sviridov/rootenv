# labenv-operator ensureRelayGrader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ensureRelayGrader` to the labenv-operator controller so every `LabEnvironment` also gets a `relay-grader` Deployment (plus a placeholder tasks ConfigMap, Service, Ingress, NetworkPolicy) in its lab namespace, deployed and reachable independently of `relay-exec`.

**Architecture:** A new file `grader.go` in the `controller` package (mirroring `relay.go`) contains `ensureRelayGrader` and one sub-function per Kubernetes resource. All sub-functions follow the existing create-once pattern: Get → if NotFound, Create → IgnoreAlreadyExists. The main controller calls `ensureRelayGrader` in `reconcileCreate` alongside the existing `ensureRelay` call. `ensureRelayGrader` does not call or depend on anything in `relay.go`.

**Tech Stack:** Go 1.26, controller-runtime, k8s.io/api, Ginkgo/Gomega + envtest for tests (matches `relay.go`/`labenvironment_controller_test.go` conventions exactly).

## Global Constraints

- Module path: `github.com/alex-sviridov/rootenv/services/labenv-operator`
- All work is inside `services/labenv-operator/`
- Create-once pattern only — no update/patch on reconcile (this differs from `ensureRelayNetworkPolicy`, which uses `controllerutil.CreateOrUpdate`; grader's NetworkPolicy has fixed content with nothing that needs re-deriving on update, so create-once is used for it too, same as every other grader resource)
- `RELAY_GRADER_IMAGE` env var is required; `ensureRelayGrader` returns error if missing
- No ServiceAccount/Role/RoleBinding for relay-grader — it makes no Kubernetes API calls
- relay-grader's NetworkPolicy denies all egress (zero egress rules, but `PolicyTypeEgress` still declared)
- Fix the pre-existing typo in `services/labenv-operator/config/manager/manager.yaml` — `RELAY_GRADER_IMAGE` currently reads ConfigMap key `grafer`, must be `grader`
- Commit after each task's tests pass, per this repo's commit policy (logical units, `<type>: <what>` format) — each task ends with a commit step

---

### Task 1: Fix `RELAY_GRADER_IMAGE` ConfigMap key typo

**Files:**
- Modify: `services/labenv-operator/config/manager/manager.yaml:111`

**Interfaces:**
- Produces: correct `RELAY_GRADER_IMAGE` env wiring, consumed by `loadGraderConfig` (Task 2)

- [ ] **Step 1: Fix the typo**

In `services/labenv-operator/config/manager/manager.yaml`, find:

```yaml
          - name: RELAY_GRADER_IMAGE
            valueFrom:
              configMapKeyRef:
                name: relay-images
                key: grafer
```

Change `key: grafer` to `key: grader`:

```yaml
          - name: RELAY_GRADER_IMAGE
            valueFrom:
              configMapKeyRef:
                name: relay-images
                key: grader
```

- [ ] **Step 2: Verify the key now matches the ConfigMap**

```bash
grep -A1 "grader:" deploy/base/55-relay-images.yaml
grep -B2 "key: grader" services/labenv-operator/config/manager/manager.yaml
```

Expected: both show `grader` (not `grafer`).

- [ ] **Step 3: Commit**

```bash
git add services/labenv-operator/config/manager/manager.yaml
git commit -m "fix(labenv-operator): correct RELAY_GRADER_IMAGE ConfigMap key typo (grafer -> grader)"
```

---

### Task 2: Create `grader.go` with `ensureRelayGrader` and all sub-functions

**Files:**
- Create: `services/labenv-operator/internal/controller/grader.go`
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller.go` (add call site only)

**Interfaces:**
- Consumes: `labv1alpha1.LabEnvironment` (existing type, `env.Name`, `env.Spec.OwnerId`), `LabEnvironmentReconciler` (existing type with embedded `client.Client`)
- Produces: `func (r *LabEnvironmentReconciler) ensureRelayGrader(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error`

- [ ] **Step 1: Create `grader.go`**

Create `services/labenv-operator/internal/controller/grader.go` with the following complete content:

```go
package controller

import (
	"context"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	labv1alpha1 "github.com/alex-sviridov/rootenv/services/labenv-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create

const graderTasksPlaceholder = `[{"id": "task1", "type": "term", "template": "echo hi"}]`

type graderConfig struct {
	image                      string
	ingressClass               string
	ingressBasePath            string
	ingressAnnotations         map[string]string
	ingressControllerNamespace string
}

func loadGraderConfig() (graderConfig, error) {
	image := os.Getenv("RELAY_GRADER_IMAGE")
	if image == "" {
		return graderConfig{}, fmt.Errorf("RELAY_GRADER_IMAGE env var is required")
	}

	basePath := os.Getenv("RELAY_GRADER_INGRESS_BASE_PATH")
	if basePath == "" {
		basePath = "/relay/grade"
	}

	ingressControllerNS := os.Getenv("RELAY_INGRESS_CONTROLLER_NAMESPACE")
	if ingressControllerNS == "" {
		ingressControllerNS = defaultIngressControllerNS
	}

	annotations := map[string]string{
		"traefik.ingress.kubernetes.io/router.middlewares": ingressControllerNS + "-relay-auth-middleware@kubernetescrd",
	}

	return graderConfig{
		image:                      image,
		ingressClass:               os.Getenv("RELAY_INGRESS_CLASS"),
		ingressBasePath:            basePath,
		ingressAnnotations:         annotations,
		ingressControllerNamespace: ingressControllerNS,
	}, nil
}

func (r *LabEnvironmentReconciler) ensureRelayGrader(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	cfg, err := loadGraderConfig()
	if err != nil {
		return err
	}

	if err := r.ensureGraderTasksConfigMap(ctx, nsName); err != nil {
		return err
	}
	if err := r.ensureRelayGraderDeployment(ctx, env, nsName, cfg); err != nil {
		return err
	}
	if err := r.ensureRelayGraderService(ctx, nsName); err != nil {
		return err
	}
	if err := r.ensureRelayGraderIngress(ctx, env, nsName, cfg); err != nil {
		return err
	}
	if err := r.ensureRelayGraderNetworkPolicy(ctx, nsName, cfg); err != nil {
		return err
	}
	return nil
}

func (r *LabEnvironmentReconciler) ensureGraderTasksConfigMap(ctx context.Context, nsName string) error {
	var existing corev1.ConfigMap
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grader-tasks",
			Namespace: nsName,
		},
		Data: map[string]string{
			"tasks.json": graderTasksPlaceholder,
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &cm))
}

func (r *LabEnvironmentReconciler) ensureRelayGraderDeployment(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string, cfg graderConfig) error {
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-grader",
			Namespace: nsName,
			Labels:    map[string]string{"app": "relay-grader"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-grader"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "relay-grader"},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(int64(10001)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "relay-grader",
							Image:           cfg.image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "RELAY_MY_NAMESPACE", Value: nsName},
								{Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
								{Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
								{Name: "RELAY_TASKS_FILE", Value: "/etc/grader/tasks.json"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "grader-tasks",
									MountPath: "/etc/grader",
									ReadOnly:  true,
								},
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
									corev1.ResourceCPU:    resource.MustParse("20m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("96Mi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "grader-tasks",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "grader-tasks"},
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

func (r *LabEnvironmentReconciler) ensureRelayGraderService(ctx context.Context, nsName string) error {
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-grader",
			Namespace: nsName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "relay-grader"},
			Ports: []corev1.ServicePort{
				{Port: 8080, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &svc))
}

func (r *LabEnvironmentReconciler) ensureRelayGraderIngress(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string, cfg graderConfig) error {
	var existing networkingv1.Ingress
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &existing)
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
			Name:      "relay-grader",
			Namespace: nsName,
		},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{}},
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
											Name: "relay-grader",
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

func (r *LabEnvironmentReconciler) ensureRelayGraderNetworkPolicy(ctx context.Context, nsName string, cfg graderConfig) error {
	var existing networkingv1.NetworkPolicy
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-relay-grader"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	tcp := corev1.ProtocolTCP
	wsPort := intstr.FromInt32(8080)

	np := networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networkpolicy-relay-grader",
			Namespace: nsName,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-grader"},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": cfg.ingressControllerNamespace,
							},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &wsPort},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &np))
}
```

- [ ] **Step 2: Add the `ensureRelayGrader` call in `reconcileCreate`**

In `services/labenv-operator/internal/controller/labenvironment_controller.go`, find the block calling `ensureRelay`:

```go
	if err := r.ensureRelay(ctx, env, nsName); err != nil {
		return ctrl.Result{}, err
	}
```

Add the grader call directly after it:

```go
	if err := r.ensureRelay(ctx, env, nsName); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureRelayGrader(ctx, env, nsName); err != nil {
		return ctrl.Result{}, err
	}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd services/labenv-operator && go build ./...
```

Expected: no output (clean build).

- [ ] **Step 4: Commit**

```bash
git add services/labenv-operator/internal/controller/grader.go services/labenv-operator/internal/controller/labenvironment_controller.go
git commit -m "feat(labenv-operator): add ensureRelayGrader, deploy relay-grader per LabEnvironment"
```

---

### Task 3: Add envtest coverage for `ensureRelayGrader`

**Files:**
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller_test.go`

**Interfaces:**
- Consumes: `ensureRelayGrader`, `graderConfig`, `loadGraderConfig` (Task 2), `k8sClient` (existing envtest fixture from `suite_test.go`)

- [ ] **Step 1: Add a `Describe("ensureRelayGrader", ...)` block**

Add this block to `services/labenv-operator/internal/controller/labenvironment_controller_test.go`, after the existing `Describe("ensureRelay", ...)` block (imports `appsv1`, `corev1`, `networkingv1`, `client`, `metav1` are already present in the file):

```go
var _ = Describe("ensureRelayGrader", func() {
	ctx := context.Background()

	BeforeEach(func() {
		DeferCleanup(func() {
			Expect(os.Unsetenv("RELAY_GRADER_IMAGE")).To(Succeed())
			Expect(os.Unsetenv("RELAY_GRADER_INGRESS_BASE_PATH")).To(Succeed())
		})
		Expect(os.Setenv("RELAY_GRADER_IMAGE", "relay-grader:test")).To(Succeed())
	})

	It("creates all relay-grader resources", func() {
		envName := "grader-resources-test"
		nsName := "rootenv-lab-" + envName
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: labv1alpha1.LabEnvironmentSpec{
				OwnerId: "usr-test",
				LabId:   "test-lab",
				Assets:  []labv1alpha1.Asset{{Name: "main", Image: "busybox"}},
			},
		}

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayGrader(ctx, env, nsName)).To(Succeed())

		By("ConfigMap grader-tasks exists with placeholder tasks.json")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &cm)).To(Succeed())
		Expect(cm.Data).To(HaveKey("tasks.json"))
		Expect(cm.Data["tasks.json"]).To(ContainSubstring("task1"))

		By("Deployment relay-grader exists with correct image, env, and volume mount")
		var deploy appsv1.Deployment
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &deploy)).To(Succeed())
		Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("relay-grader:test"))
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
			corev1.EnvVar{Name: "RELAY_MY_NAMESPACE", Value: nsName},
			corev1.EnvVar{Name: "RELAY_MY_ATTEMPT_ID", Value: envName},
			corev1.EnvVar{Name: "RELAY_MY_OWNER_ID", Value: "usr-test"},
			corev1.EnvVar{Name: "RELAY_TASKS_FILE", Value: "/etc/grader/tasks.json"},
		))
		Expect(deploy.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(
			corev1.VolumeMount{Name: "grader-tasks", MountPath: "/etc/grader", ReadOnly: true},
		))
		Expect(deploy.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())

		By("Service relay-grader exists")
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &svc)).To(Succeed())
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))
		Expect(svc.Spec.Selector).To(HaveKeyWithValue("app", "relay-grader"))

		By("Ingress relay-grader exists with correct path")
		var ing networkingv1.Ingress
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &ing)).To(Succeed())
		Expect(ing.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/relay/grade/" + envName))
		Expect(ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal("relay-grader"))

		By("NetworkPolicy networkpolicy-relay-grader exists with deny-all egress")
		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-relay-grader"}, &np)).To(Succeed())
		Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "relay-grader"))
		Expect(np.Spec.PolicyTypes).To(ConsistOf(
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		))
		Expect(np.Spec.Egress).To(BeEmpty())
		Expect(np.Spec.Ingress).To(HaveLen(1))
		Expect(np.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(8080))

		By("ensureRelayGrader is idempotent — second call does not error")
		Expect(r.ensureRelayGrader(ctx, env, nsName)).To(Succeed())
	})

	It("returns error when RELAY_GRADER_IMAGE is missing", func() {
		Expect(os.Unsetenv("RELAY_GRADER_IMAGE")).To(Succeed())
		envName := "grader-missing-image-test"
		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: labv1alpha1.LabEnvironmentSpec{
				OwnerId: "usr-test",
				LabId:   "lab",
				Assets:  []labv1alpha1.Asset{{Name: "m", Image: "b"}},
			},
		}
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayGrader(ctx, env, "rootenv-lab-"+envName)).To(MatchError(ContainSubstring("RELAY_GRADER_IMAGE")))
	})
})

var _ = Describe("loadGraderConfig", func() {
	AfterEach(func() {
		Expect(os.Unsetenv("RELAY_GRADER_IMAGE")).To(Succeed())
		Expect(os.Unsetenv("RELAY_GRADER_INGRESS_BASE_PATH")).To(Succeed())
	})

	It("returns error when RELAY_GRADER_IMAGE is unset", func() {
		_, err := loadGraderConfig()
		Expect(err).To(MatchError(ContainSubstring("RELAY_GRADER_IMAGE")))
	})

	It("uses /relay/grade as default base path", func() {
		Expect(os.Setenv("RELAY_GRADER_IMAGE", "img:tag")).To(Succeed())
		cfg, err := loadGraderConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressBasePath).To(Equal("/relay/grade"))
	})

	It("includes the relay auth middleware annotation by default", func() {
		Expect(os.Setenv("RELAY_GRADER_IMAGE", "img:tag")).To(Succeed())
		cfg, err := loadGraderConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressAnnotations).To(HaveKeyWithValue(
			"traefik.ingress.kubernetes.io/router.middlewares",
			"kube-system-relay-auth-middleware@kubernetescrd",
		))
	})
})
```

- [ ] **Step 2: Run tests**

```bash
cd services/labenv-operator && make test 2>&1 | tail -40
```

Expected: all tests pass, including the new `ensureRelayGrader` and `loadGraderConfig` describe blocks.

- [ ] **Step 3: Verify RBAC markers generate correctly**

```bash
cd services/labenv-operator && make manifests 2>&1
grep -n "configmaps" config/rbac/role.yaml
```

Expected: `make manifests` exits 0; `config/rbac/role.yaml` now contains a rule for `configmaps` (verbs `get`, `list`, `watch`, `create`).

- [ ] **Step 4: Commit**

```bash
git add services/labenv-operator/internal/controller/labenvironment_controller_test.go services/labenv-operator/config/rbac/role.yaml
git commit -m "test(labenv-operator): add envtest coverage for ensureRelayGrader"
```

---

## Self-Review Notes

- **Spec coverage:** ConfigMap `grader-tasks` with placeholder content (spec: "ConfigMap per LabEnvironment, static placeholder content") — Task 2. No ServiceAccount/Role/RoleBinding (spec: "No ServiceAccount/Role for grader") — Task 2, deployment has no `ServiceAccountName` set, confirmed by test assertion `ServiceAccountName` is empty. Separate Ingress object (spec: "Separate Ingress resource") — Task 2 `ensureRelayGraderIngress` creates its own `relay-grader` Ingress, no coupling to `ensureRelayIngress`. `RELAY_GRADER_INGRESS_BASE_PATH` env var defaulting to `/relay/grade` (spec) — Task 2 `loadGraderConfig`. Deny-all egress NetworkPolicy (spec) — Task 2 `ensureRelayGraderNetworkPolicy`, `Egress: []networkingv1.NetworkPolicyEgressRule{}` with `PolicyTypeEgress` declared. `manager.yaml` typo fix (spec) — Task 1. Call site wired into `reconcileCreate` (spec) — Task 2 Step 2.
- **Type consistency:** `graderConfig{image, ingressClass, ingressBasePath, ingressAnnotations, ingressControllerNamespace}` defined in Task 2 Step 1 is used identically by `ensureRelayGraderDeployment`, `ensureRelayGraderIngress`, `ensureRelayGraderNetworkPolicy` in the same step, and by the test assertions in Task 3. `RELAY_TASKS_FILE=/etc/grader/tasks.json` matches the volume `MountPath: /etc/grader` + ConfigMap key `tasks.json` exactly.
- **No placeholders:** every step has complete, runnable Go code and exact commands; no TBD/TODO.
- **Reused existing symbols:** `defaultIngressControllerNS` (defined in `relay.go`) is reused directly in `grader.go` rather than redefined — package-level const, no import needed since same package.
