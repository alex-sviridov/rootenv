package controller

import (
	"context"
	"encoding/json"
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

	if err := r.ensureGraderTasksConfigMap(ctx, env, nsName); err != nil {
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

func (r *LabEnvironmentReconciler) ensureGraderTasksConfigMap(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	var existing corev1.ConfigMap
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	type graderTask struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Template string `json:"template"`
		Asset    string `json:"asset,omitempty"`
	}
	tasks := make([]graderTask, 0, len(env.Spec.Exercises))
	for _, ex := range env.Spec.Exercises {
		tasks = append(tasks, graderTask{
			ID:       ex.ID,
			Type:     ex.Type,
			Template: ex.Template,
			Asset:    ex.Asset,
		})
	}
	tasksJSON, err := json.Marshal(tasks)
	if err != nil {
		return fmt.Errorf("marshal grader tasks: %w", err)
	}

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grader-tasks",
			Namespace: nsName,
		},
		Data: map[string]string{
			"tasks.json": string(tasksJSON),
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
