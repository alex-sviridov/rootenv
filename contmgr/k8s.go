package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

// podWatchTimeoutSec is the server-side timeout passed to the pod Watch call.
// Declared as a var so its address can be taken for the k8s ListOptions field.
var podWatchTimeoutSec = int64(120)

func boolPtr(b bool) *bool { return &b }

// k8sDoer is the Kubernetes operations contmgr needs.
type k8sDoer interface {
	EnsureNamespace(ctx context.Context, p NamespaceParams) error
	EnsureRoleBinding(ctx context.Context, namespace string) error
	DeleteNamespace(ctx context.Context, namespace string) error
	EnsureNetworkPolicy(ctx context.Context, p NetPolParams) error
	EnsureHeadlessService(ctx context.Context, namespace, assetName string) error
	CreatePod(ctx context.Context, p PodParams) error
	CreateService(ctx context.Context, p PodParams) error
	WaitPodRunning(ctx context.Context, namespace, podName string) error
	ExecInPod(ctx context.Context, namespace, podName string, cmd []string) error
	DeletePod(ctx context.Context, namespace, podName string) error
	DeleteService(ctx context.Context, namespace, svcName string) error
	DeleteNetworkPolicy(ctx context.Context, namespace, netpolName string) error
}

type NamespaceParams struct {
	Name      string
	AttemptID string
	UserID    string
	LabID     string
	UserEmail string
	CreatedAt string
	ExpiresAt string
}

type NetPolParams struct {
	Namespace      string
	InfraNamespace string
}

type PodParams struct {
	Namespace       string
	UserID          string
	AttemptID       string
	AssetName       string
	Image           string
	SSHUser         string
	CPU             string // e.g. "1" or "500m"
	Memory          string // e.g. "512MB"
	Disk            string // e.g. "5GB"; empty = no limit
	ImagePullSecret string // empty = omit
	RuntimeClass    string // empty = cluster default; "gvisor" = gVisor
}

type K8sClient struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
}

func newK8sClient() (*K8sClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loadRules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadRules, nil).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s config: %w", err)
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s clientset: %w", err)
	}
	return &K8sClient{clientset: cs, restConfig: cfg}, nil
}

func (k *K8sClient) EnsureNamespace(ctx context.Context, p NamespaceParams) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: p.Name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "rootenv-contmgr",
				"rootenv.io/session-id":        p.AttemptID,
				"rootenv.io/user-id":           p.UserID,
				"rootenv.io/lab-id":            p.LabID,
			},
			Annotations: map[string]string{
				"rootenv.io/user-email": p.UserEmail,
				"rootenv.io/created-at": p.CreatedAt,
				"rootenv.io/expires-at": p.ExpiresAt,
			},
		},
	}
	_, err := k.clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return err
	}
	// Namespace exists — check if it is terminating. If so, wait for it to
	// disappear before recreating; provisioning into a terminating namespace
	// is forbidden by the API server.
	existing, err := k.clientset.CoreV1().Namespaces().Get(ctx, p.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		// Deleted between Create and Get — retry create once.
		_, err = k.clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	if err != nil {
		return err
	}
	if existing.Status.Phase != corev1.NamespaceTerminating {
		return nil // exists and healthy
	}
	// Wait for the terminating namespace to be fully deleted.
	if err := k.waitNamespaceGone(ctx, p.Name); err != nil {
		return fmt.Errorf("wait for terminating namespace %s: %w", p.Name, err)
	}
	_, err = k.clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (k *K8sClient) waitNamespaceGone(ctx context.Context, name string) error {
	watcher, err := k.clientset.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{
		FieldSelector:  "metadata.name=" + name,
		TimeoutSeconds: &podWatchTimeoutSec,
	})
	if err != nil {
		return err
	}
	defer watcher.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				// Channel closed — namespace is gone or watch timed out.
				return nil
			}
			if event.Type == watch.Deleted {
				return nil
			}
		}
	}
}

func (k *K8sClient) EnsureRoleBinding(ctx context.Context, namespace string) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "contmgr", Namespace: namespace},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list", "watch", "create", "delete"}},
			{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create"}},
			{APIGroups: []string{""}, Resources: []string{"services"}, Verbs: []string{"get", "list", "create", "delete"}},
			{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "create", "delete"}},
		},
	}
	_, err := k.clientset.RbacV1().Roles(namespace).Create(ctx, role, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("create role: %w", err)
	}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "contmgr", Namespace: namespace},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "contmgr", Namespace: "rootenv-infra"}},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "contmgr"},
	}
	_, err = k.clientset.RbacV1().RoleBindings(namespace).Create(ctx, rb, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (k *K8sClient) DeleteNamespace(ctx context.Context, namespace string) error {
	if namespace == "" {
		return nil
	}
	err := k.clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

// EnsureHeadlessService creates a clusterIP=None service named after the asset
// in the given namespace. This gives the pod a stable short DNS name:
// kube-dns search path resolves "{assetName}" to
// "{assetName}.{namespace}.svc.cluster.local" automatically.
// The relay uses the separate ClusterIP service ({assetName}-svc) for SSH.
func (k *K8sClient) EnsureHeadlessService(ctx context.Context, namespace, assetName string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      assetName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector:  map[string]string{"rootenv.io/asset-name": assetName},
		},
	}
	_, err := k.clientset.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (k *K8sClient) EnsureNetworkPolicy(ctx context.Context, p NetPolParams) error {
	tcp := corev1.ProtocolTCP
	port22 := intstr.FromInt32(22)
	netpol := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-relay",
			Namespace: p.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// pod-to-pod within the same namespace
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": p.Namespace,
							},
						},
					}},
				},
				// port 22 from rootenv-infra (relay SSH access)
				{
					Ports: []networkingv1.NetworkPolicyPort{{
						Protocol: &tcp,
						Port:     &port22,
					}},
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": p.InfraNamespace,
							},
						},
					}},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// pod-to-pod within the same namespace
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": p.Namespace,
							},
						},
					}},
				},
				// DNS: port 53 to kube-dns pods only
				{
					Ports: dnsPorts(),
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "kube-system",
							},
						},
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"k8s-app": "kube-dns",
							},
						},
					}},
				},
			},
		},
	}
	_, err := k.clientset.NetworkingV1().NetworkPolicies(p.Namespace).Create(ctx, netpol, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (k *K8sClient) CreatePod(ctx context.Context, p PodParams) error {
	memBytes, err := parseMemory(p.Memory)
	if err != nil {
		return fmt.Errorf("parse memory: %w", err)
	}
	memQ := resource.NewQuantity(memBytes, resource.BinarySI)
	cpuMilli, err := parseCPUMilli(p.CPU)
	if err != nil {
		return fmt.Errorf("parse cpu: %w", err)
	}
	cpuQ := resource.NewMilliQuantity(cpuMilli, resource.DecimalSI)

	limits := corev1.ResourceList{
		corev1.ResourceCPU:    *cpuQ,
		corev1.ResourceMemory: *memQ,
	}
	if p.Disk != "" {
		diskBytes, err := parseMemory(p.Disk)
		if err != nil {
			return fmt.Errorf("parse disk: %w", err)
		}
		limits[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(diskBytes, resource.BinarySI)
	}
	// Requests at 25% CPU / 50% memory of limits for predictable scheduling.
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuMilli/4, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(memBytes/2, resource.BinarySI),
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "rootenv-contmgr",
		"rootenv.io/asset-name":        p.AssetName,
		"rootenv.io/user-id":           p.UserID,
		"rootenv.io/attempt-id":        p.AttemptID,
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName(p.AssetName),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			// hostUsers: false maps container root to an unprivileged UID on the host
			// via kernel user namespaces. Requires K8s 1.30+ with the feature gate enabled.
			HostUsers: boolPtr(false),
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{{
				Name:            p.AssetName,
				Image:           p.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Resources: corev1.ResourceRequirements{
					Limits:   limits,
					Requests: requests,
				},
				// Drop NET_RAW to block raw socket abuse (ping floods, ARP spoofing).
				// Other caps intentionally kept: RHCSA labs need SYS_ADMIN, NET_ADMIN, etc.
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"NET_RAW"},
					},
				},
			}},
		},
	}
	if p.RuntimeClass != "" {
		pod.Spec.RuntimeClassName = &p.RuntimeClass
	}
	if p.ImagePullSecret != "" {
		pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: p.ImagePullSecret}}
	}
	_, err = k.clientset.CoreV1().Pods(p.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (k *K8sClient) CreateService(ctx context.Context, p PodParams) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName(p.AssetName),
			Namespace: p.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"rootenv.io/asset-name": p.AssetName,
			},
			Ports: []corev1.ServicePort{{
				Name:       "ssh",
				Port:       22,
				TargetPort: intstr.FromInt32(22),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	_, err := k.clientset.CoreV1().Services(p.Namespace).Create(ctx, svc, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (k *K8sClient) WaitPodRunning(ctx context.Context, namespace, name string) error {
	watcher, err := k.clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:  "metadata.name=" + name,
		TimeoutSeconds: &podWatchTimeoutSec,
	})
	if err != nil {
		return fmt.Errorf("watch pod %s: %w", name, err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("pod %s watch channel closed before Running", name)
			}
			if event.Type == watch.Error {
				return fmt.Errorf("pod %s watch error event", name)
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			logPodContainerStates(pod)
			switch pod.Status.Phase {
			case corev1.PodRunning:
				return nil
			case corev1.PodFailed, corev1.PodSucceeded:
				return fmt.Errorf("pod %s ended unexpectedly: phase=%s", name, pod.Status.Phase)
			}
		}
	}
}

// logPodEarlyExit logs terminated containers with a non-zero exit code so that
// pods that start and die immediately are visible without kubectl.
func logPodEarlyExit(pod *corev1.Pod) {
	images := make(map[string]string, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		images[c.Name] = c.Image
	}
	for _, cs := range pod.Status.ContainerStatuses {
		t := cs.State.Terminated
		if t == nil {
			t = cs.LastTerminationState.Terminated
		}
		if t != nil && t.ExitCode != 0 {
			slog.Warn("pod container exited unexpectedly",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"container", cs.Name,
				"image", images[cs.Name],
				"exit_code", t.ExitCode,
				"reason", t.Reason,
				"message", t.Message,
			)
		}
	}
}

// logPodContainerStates emits a log line for each container that is waiting
// (e.g. ImagePullBackOff, ErrImagePull, CrashLoopBackOff) or terminated
// unexpectedly, so that stuck-provisioning causes are visible without kubectl.
func logPodContainerStates(pod *corev1.Pod) {
	// Build a name→image map from the spec for fast lookup.
	images := make(map[string]string, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		images[c.Name] = c.Image
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil {
			slog.Warn("pod container waiting",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"container", cs.Name,
				"image", images[cs.Name],
				"reason", w.Reason,
				"message", w.Message,
			)
		} else if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			slog.Warn("pod container terminated",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"container", cs.Name,
				"image", images[cs.Name],
				"exit_code", t.ExitCode,
				"reason", t.Reason,
				"message", t.Message,
			)
		}
	}
}

func (k *K8sClient) ExecInPod(ctx context.Context, namespace, name string, cmd []string) error {
	const maxAttempts = 5
	for attempt := range maxAttempts {
		err := k.execInPodOnce(ctx, namespace, name, cmd)
		if err == nil {
			return nil
		}
		// containerd reports "task not found" briefly after the pod reaches Running
		// while the shim is still initialising. Retry with backoff.
		if attempt < maxAttempts-1 && strings.Contains(err.Error(), "task") && strings.Contains(err.Error(), "not found") {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
			continue
		}
		return err
	}
	return fmt.Errorf("exec in pod %s/%s: unreachable", namespace, name)
}

func (k *K8sClient) execInPodOnce(ctx context.Context, namespace, name string, cmd []string) error {
	req := k.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: cmd,
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k.restConfig, "POST", req.URL())
	if err != nil {
		return err
	}
	var stderr strings.Builder
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: io.Discard,
		Stderr: &stderr,
	})
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func (k *K8sClient) DeletePod(ctx context.Context, namespace, name string) error {
	if name == "" {
		return nil
	}
	err := k.clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (k *K8sClient) DeleteService(ctx context.Context, namespace, name string) error {
	if name == "" {
		return nil
	}
	err := k.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (k *K8sClient) DeleteNetworkPolicy(ctx context.Context, namespace, name string) error {
	if name == "" {
		return nil
	}
	err := k.clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

