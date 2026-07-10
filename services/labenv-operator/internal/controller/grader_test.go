/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	labv1alpha1 "github.com/alex-sviridov/rootenv/services/labenv-operator/api/v1alpha1"
)

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
				Exercises: []labv1alpha1.Exercise{
					{ID: "1.1", Description: "Create a file", Type: "term", Asset: "main", Template: "test -f /tmp/x"},
					{ID: "1.2", Description: "No asset filter", Type: "term", Template: "echo hi"},
				},
			},
		}

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayGrader(ctx, env, nsName)).To(Succeed())

		By("ConfigMap grader-tasks exists with tasks.json derived from spec.exercises")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &cm)).To(Succeed())
		Expect(cm.Data).To(HaveKey("tasks.json"))
		var tasks []map[string]any
		Expect(json.Unmarshal([]byte(cm.Data["tasks.json"]), &tasks)).To(Succeed())
		Expect(tasks).To(HaveLen(2))
		Expect(tasks[0]["id"]).To(Equal("1.1"))
		Expect(tasks[0]["type"]).To(Equal("term"))
		Expect(tasks[0]["template"]).To(Equal("test -f /tmp/x"))
		Expect(tasks[0]["asset"]).To(Equal("main"))
		Expect(tasks[0]).NotTo(HaveKey("description"))
		Expect(tasks[1]).NotTo(HaveKey("asset"))

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
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RELAY_GRADER_INTERNAL_PORT", Value: "8081"},
		))
		Expect(deploy.Spec.Template.Spec.Containers[0].Ports).To(ContainElement(
			corev1.ContainerPort{ContainerPort: 8081, Protocol: corev1.ProtocolTCP},
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

		By("Service relay-grader exposes the internal forwarder port")
		Expect(svc.Spec.Ports).To(ContainElement(
			corev1.ServicePort{Name: "internal", Protocol: corev1.ProtocolTCP, Port: 8081, TargetPort: intstr.FromInt32(8081)},
		))

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
		Expect(np.Spec.Ingress).To(HaveLen(2))
		Expect(np.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(8080))

		By("NetworkPolicy allows ingress from relay-exec pods on the internal port")
		var foundExecRule bool
		for _, rule := range np.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.PodSelector != nil && peer.PodSelector.MatchLabels["app"] == "relay-exec" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(8081))
					foundExecRule = true
				}
			}
		}
		Expect(foundExecRule).To(BeTrue(), "expected ingress rule allowing relay-exec pods on port 8081")

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

	It("writes an empty tasks.json array when spec.exercises is empty", func() {
		envName := "grader-empty-exercises-test"
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

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &cm)).To(Succeed())
		Expect(cm.Data["tasks.json"]).To(Equal("[]"))
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

	It("uses RELAY_INGRESS_CONTROLLER_NAMESPACE override for the middleware annotation and controller namespace", func() {
		Expect(os.Setenv("RELAY_GRADER_IMAGE", "img:tag")).To(Succeed())
		Expect(os.Setenv("RELAY_INGRESS_CONTROLLER_NAMESPACE", "traefik-system")).To(Succeed())
		DeferCleanup(func() { Expect(os.Unsetenv("RELAY_INGRESS_CONTROLLER_NAMESPACE")).To(Succeed()) })

		cfg, err := loadGraderConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressControllerNamespace).To(Equal("traefik-system"))
		Expect(cfg.ingressAnnotations).To(HaveKeyWithValue(
			"traefik.ingress.kubernetes.io/router.middlewares",
			"traefik-system-relay-auth-middleware@kubernetescrd",
		))
	})
})

var _ = Describe("ensureRelayGraderNetworkPolicy", func() {
	ctx := context.Background()

	It("creates the network policy with correct shape using a custom ingress controller namespace", func() {
		nsName := "rootenv-lab-grader-np-shape-test"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cfg := graderConfig{ingressControllerNamespace: "traefik-system"}
		Expect(r.ensureRelayGraderNetworkPolicy(ctx, nsName, cfg)).To(Succeed())

		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-relay-grader"}, &np)).To(Succeed())

		By("pod selector targets relay-grader only")
		Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "relay-grader"))

		By("both Ingress and Egress policy types are set, egress is deny-all")
		Expect(np.Spec.PolicyTypes).To(ConsistOf(
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		))
		Expect(np.Spec.Egress).To(BeEmpty())

		By("ingress allows port 8080 from the configured ingress controller namespace")
		var foundIngressControllerRule bool
		for _, rule := range np.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.NamespaceSelector != nil && peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "traefik-system" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(8080))
					foundIngressControllerRule = true
				}
			}
		}
		Expect(foundIngressControllerRule).To(BeTrue(), "expected ingress rule allowing traefik-system on port 8080")

		By("ingress allows port 8081 from relay-exec pods only")
		var foundExecRule bool
		for _, rule := range np.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.PodSelector != nil && peer.PodSelector.MatchLabels["app"] == "relay-exec" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(8081))
					foundExecRule = true
				}
			}
		}
		Expect(foundExecRule).To(BeTrue(), "expected ingress rule allowing relay-exec pods on port 8081")
	})

	It("is idempotent — second call does not error", func() {
		nsName := "rootenv-lab-grader-np-idempotent-test"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cfg := graderConfig{ingressControllerNamespace: "kube-system"}
		Expect(r.ensureRelayGraderNetworkPolicy(ctx, nsName, cfg)).To(Succeed())
		Expect(r.ensureRelayGraderNetworkPolicy(ctx, nsName, cfg)).To(Succeed())
	})
})
