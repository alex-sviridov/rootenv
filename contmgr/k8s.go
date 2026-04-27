package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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

// k8sDoer is the Kubernetes operations contmgr needs.
type k8sDoer interface {
	EnsureNetworkPolicy(ctx context.Context, p NetPolParams) error
	CreatePod(ctx context.Context, p PodParams) error
	CreateService(ctx context.Context, p PodParams) error
	WaitPodRunning(ctx context.Context, namespace, podName string) error
	ExecInPod(ctx context.Context, namespace, podName string, cmd []string) error
	DeletePod(ctx context.Context, namespace, podName string) error
	DeleteService(ctx context.Context, namespace, svcName string) error
	DeleteNetworkPolicy(ctx context.Context, namespace, netpolName string) error
}

type NetPolParams struct {
	Namespace      string
	UserID         string
	AttemptID      string
	InfraNamespace string
}

type PodParams struct {
	Namespace       string
	UserID          string
	AttemptID       string
	AssetName       string
	Image           string
	SSHUser         string
	CPU             string // e.g. "1" or "0.5"
	Memory          string // e.g. "512MB"
	ImagePullSecret string // empty = omit
}

type K8sClient struct {
	clientset  *kubernetes.Clientset
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

// --- resource naming ---

func netpolName(userID, attemptID string) string {
	return userID + "-" + attemptID + "-netpol"
}

func podName(userID, attemptID, assetName string) string {
	return userID + "-" + attemptID + "-" + assetName
}

func svcName(userID, attemptID, assetName string) string {
	return userID + "-" + attemptID + "-" + assetName + "-svc"
}

func svcDNS(svc, namespace string) string {
	return svc + "." + namespace + ".svc.cluster.local"
}

// --- EnsureNetworkPolicy ---

func (k *K8sClient) EnsureNetworkPolicy(ctx context.Context, p NetPolParams) error {
	tcp := corev1.ProtocolTCP
	port22 := intstr.FromInt32(22)
	netpol := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netpolName(p.UserID, p.AttemptID),
			Namespace: p.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"user-id":    p.UserID,
					"attempt-id": p.AttemptID,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// pod-to-pod within same attempt, same namespace only
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": p.Namespace,
							},
						},
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"user-id":    p.UserID,
								"attempt-id": p.AttemptID,
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
				// pod-to-pod within same attempt, same namespace only
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": p.Namespace,
							},
						},
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"user-id":    p.UserID,
								"attempt-id": p.AttemptID,
							},
						},
					}},
				},
				// DNS: allow egress to kube-dns in kube-system
				{
					Ports: dnsPorts(),
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": "kube-system",
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

// --- CreatePod ---

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

	labels := map[string]string{
		"user-id":    p.UserID,
		"attempt-id": p.AttemptID,
		"asset-name": p.AssetName,
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName(p.UserID, p.AttemptID, p.AssetName),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:            p.AssetName,
				Image:           p.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{{
					Name:  "SSH_USERS",
					Value: p.SSHUser + ":1000:1000",
				}},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    *cpuQ,
						corev1.ResourceMemory: *memQ,
					},
				},
			}},
		},
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

// --- CreateService ---

func (k *K8sClient) CreateService(ctx context.Context, p PodParams) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName(p.UserID, p.AttemptID, p.AssetName),
			Namespace: p.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"user-id":    p.UserID,
				"attempt-id": p.AttemptID,
				"asset-name": p.AssetName,
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

// --- WaitPodRunning ---

func (k *K8sClient) WaitPodRunning(ctx context.Context, namespace, name string) error {
	timeout := int64(120)
	watcher, err := k.clientset.CoreV1().Pods(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:  "metadata.name=" + name,
		TimeoutSeconds: &timeout,
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
			switch pod.Status.Phase {
			case corev1.PodRunning:
				return nil
			case corev1.PodFailed, corev1.PodSucceeded:
				return fmt.Errorf("pod %s ended unexpectedly: phase=%s", name, pod.Status.Phase)
			}
		}
	}
}

// --- ExecInPod ---

func (k *K8sClient) ExecInPod(ctx context.Context, namespace, name string, cmd []string) error {
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

// --- Delete methods ---

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

// --- CPU / memory helpers ---

func dnsPorts() []networkingv1.NetworkPolicyPort {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	port53 := intstr.FromInt32(53)
	return []networkingv1.NetworkPolicyPort{
		{Protocol: &udp, Port: &port53},
		{Protocol: &tcp, Port: &port53},
	}
}

// parseCPUMilli converts a CPU string like "1", "0.5", or "500m" to millicores.
func parseCPUMilli(cpu string) (int64, error) {
	if cpu == "" {
		return 0, nil
	}
	if strings.HasSuffix(cpu, "m") {
		var v int64
		if _, err := fmt.Sscanf(cpu[:len(cpu)-1], "%d", &v); err != nil {
			return 0, fmt.Errorf("unrecognized cpu millicore format: %q", cpu)
		}
		return v, nil
	}
	var v float64
	if _, err := fmt.Sscanf(cpu, "%f", &v); err != nil {
		return 0, fmt.Errorf("unrecognized cpu format: %q", cpu)
	}
	return int64(v * 1000), nil
}

// parseMemory converts strings like "512MB", "1GB", "256m" to bytes.
func parseMemory(mem string) (int64, error) {
	if mem == "" {
		return 0, nil
	}
	mem = strings.TrimSpace(mem)
	upper := strings.ToUpper(mem)
	units := map[string]int64{
		"GB": 1 << 30,
		"MB": 1 << 20,
		"KB": 1 << 10,
		"G":  1 << 30,
		"M":  1 << 20,
		"K":  1 << 10,
	}
	for suffix, mult := range units {
		if strings.HasSuffix(upper, suffix) {
			var v int64
			fmt.Sscanf(mem[:len(mem)-len(suffix)], "%d", &v)
			return v * mult, nil
		}
	}
	var v int64
	if _, err := fmt.Sscanf(mem, "%d", &v); err != nil {
		return 0, fmt.Errorf("unrecognized memory format: %q", mem)
	}
	return v, nil
}
