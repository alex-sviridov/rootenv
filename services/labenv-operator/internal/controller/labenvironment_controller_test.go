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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	labv1alpha1 "github.com/alex-sviridov/rootenv/services/labenv-operator/api/v1alpha1"
)

var _ = Describe("LabEnvironment Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		labenvironment := &labv1alpha1.LabEnvironment{}

		BeforeEach(func() {
			os.Setenv("RELAY_IMAGE", "relay-primitive:test")
			DeferCleanup(func() { os.Unsetenv("RELAY_IMAGE") })

			By("creating the custom resource for the Kind LabEnvironment")
			err := k8sClient.Get(ctx, typeNamespacedName, labenvironment)
			if err != nil && errors.IsNotFound(err) {
				resource := &labv1alpha1.LabEnvironment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: labv1alpha1.LabEnvironmentSpec{
						OwnerId: "test-owner",
						LabId:   "test-lab",
						Assets: []labv1alpha1.Asset{
							{
								Name:  "main",
								Image: "busybox",
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &labv1alpha1.LabEnvironment{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance LabEnvironment")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LabEnvironmentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

var _ = Describe("ensureRelay", func() {
	const envName = "relay-test-env"
	const nsName = "rootenv-lab-" + envName
	ctx := context.Background()

	BeforeEach(func() {
		DeferCleanup(func() {
			os.Unsetenv("RELAY_IMAGE")
			os.Unsetenv("RELAY_INGRESS_CLASS")
			os.Unsetenv("RELAY_INGRESS_BASE_PATH")
			os.Unsetenv("RELAY_INGRESS_ANNOTATIONS")
		})
		os.Setenv("RELAY_IMAGE", "relay-exec:test")
		os.Setenv("RELAY_INGRESS_CLASS", "traefik")
		os.Setenv("RELAY_INGRESS_BASE_PATH", "/relay/exec")
		os.Setenv("RELAY_INGRESS_ANNOTATIONS", "traefik.ingress.kubernetes.io/router.entrypoints=websecure")

		By("creating the lab namespace")
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
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

		By("Deployment relay-exec exists with correct image and env")
		var deploy appsv1.Deployment
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-exec"}, &deploy)).To(Succeed())
		Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("relay-exec:test"))
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
			corev1.EnvVar{Name: "RELAY_MY_NAMESPACE", Value: nsName},
			corev1.EnvVar{Name: "RELAY_MY_ATTEMPT_ID", Value: envName},
			corev1.EnvVar{Name: "RELAY_MY_OWNER_ID", Value: "usr-test"},
			corev1.EnvVar{Name: "RELAY_SKIP_AUTH", Value: "true"},
		))

		By("Service relay exists")
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &svc)).To(Succeed())
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))

		By("Ingress relay exists with correct path and annotation")
		var ing networkingv1.Ingress
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
		Expect(ing.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/relay/exec/" + envName))
		Expect(ing.Annotations).To(HaveKey("traefik.ingress.kubernetes.io/router.entrypoints"))
		Expect(*ing.Spec.IngressClassName).To(Equal("traefik"))

		By("NetworkPolicy networkpolicy-relay-exec exists with correct pod selector")
		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-relay-exec"}, &np)).To(Succeed())
		Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "relay-exec"))

		By("ensureRelay is idempotent — second call does not error")
		Expect(r.ensureRelay(ctx, env, nsName)).To(Succeed())
	})

	It("returns error when RELAY_IMAGE is missing", func() {
		os.Unsetenv("RELAY_IMAGE")
		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: labv1alpha1.LabEnvironmentSpec{
				OwnerId: "usr-test",
				LabId:   "lab",
				Assets:  []labv1alpha1.Asset{{Name: "m", Image: "b"}},
			},
		}
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelay(ctx, env, nsName)).To(MatchError(ContainSubstring("RELAY_IMAGE")))
	})
})

var _ = Describe("apiServerEndpoint", func() {
	ctx := context.Background()
	r := &LabEnvironmentReconciler{}

	BeforeEach(func() {
		r = &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	})

	It("returns the IP and port from the kubernetes Endpoints", func() {
		ip, port, err := r.apiServerEndpoint(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).NotTo(BeEmpty())
		Expect(port).To(BeNumerically(">", 0))
	})
})

var _ = Describe("ensureRelayNetworkPolicy", func() {
	const envName = "np-test-env"
	const nsName = "rootenv-lab-" + envName
	ctx := context.Background()

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		_ = k8sClient.Delete(ctx, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: nsName, Name: "networkpolicy-relay-exec"}})
	})

	It("creates the network policy with correct shape", func() {
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		ip, port, err := r.apiServerEndpoint(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(r.ensureRelayNetworkPolicy(ctx, nsName)).To(Succeed())

		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-relay-exec"}, &np)).To(Succeed())

		By("pod selector targets relay-exec only")
		Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "relay-exec"))

		By("both Ingress and Egress policy types are set")
		Expect(np.Spec.PolicyTypes).To(ConsistOf(
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		))

		By("ingress allows port 8080 from kube-system only")
		Expect(np.Spec.Ingress).To(HaveLen(1))
		ingressRule := np.Spec.Ingress[0]
		Expect(ingressRule.Ports).To(HaveLen(1))
		Expect(ingressRule.Ports[0].Port.IntValue()).To(Equal(8080))
		Expect(ingressRule.From).To(HaveLen(1))
		Expect(ingressRule.From[0].NamespaceSelector.MatchLabels).To(HaveKeyWithValue(
			"kubernetes.io/metadata.name", "kube-system",
		))

		By("egress has same-namespace pods rule (excluding relay-exec)")
		var foundPodRule bool
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil && peer.PodSelector != nil {
					Expect(peer.NamespaceSelector.MatchLabels).To(HaveKeyWithValue(
						"kubernetes.io/metadata.name", nsName,
					))
					Expect(peer.PodSelector.MatchExpressions).To(HaveLen(1))
					Expect(peer.PodSelector.MatchExpressions[0].Key).To(Equal("app"))
					Expect(peer.PodSelector.MatchExpressions[0].Operator).To(Equal(metav1.LabelSelectorOpNotIn))
					Expect(peer.PodSelector.MatchExpressions[0].Values).To(ConsistOf("relay-exec"))
					foundPodRule = true
				}
			}
		}
		Expect(foundPodRule).To(BeTrue(), "expected egress rule allowing same-namespace non-relay-exec pods")

		By("egress contains apiserver ipBlock with real post-DNAT IP and port")
		var foundAPIRule bool
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.IPBlock != nil && peer.IPBlock.CIDR == ip+"/32" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(int(port)))
					foundAPIRule = true
				}
			}
		}
		Expect(foundAPIRule).To(BeTrue(), "expected egress rule with ipBlock %s/32 port %d", ip, port)
	})

	It("is idempotent — second call updates without error", func() {
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayNetworkPolicy(ctx, nsName)).To(Succeed())
		Expect(r.ensureRelayNetworkPolicy(ctx, nsName)).To(Succeed())
	})
})

var _ = Describe("ensureNetworkPolicy (denyall)", func() {
	const nsName = "rootenv-lab-denyall-test"
	ctx := context.Background()

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
	})

	It("creates denyall policy with correct shape", func() {
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureNetworkPolicy(ctx, nsName)).To(Succeed())

		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-denyall"}, &np)).To(Succeed())

		By("pod selector is empty (applies to all pods)")
		Expect(np.Spec.PodSelector).To(Equal(metav1.LabelSelector{}))

		By("both policy types set")
		Expect(np.Spec.PolicyTypes).To(ConsistOf(
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		))

		By("ingress allows same-namespace traffic")
		var foundSameNS bool
		for _, rule := range np.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.NamespaceSelector != nil &&
					peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == nsName {
					foundSameNS = true
				}
			}
		}
		Expect(foundSameNS).To(BeTrue(), "expected ingress rule allowing same-namespace traffic")

		By("ingress allows kube-system on port 8080")
		var foundTraefik bool
		for _, rule := range np.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.NamespaceSelector != nil &&
					peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "kube-system" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(8080))
					foundTraefik = true
				}
			}
		}
		Expect(foundTraefik).To(BeTrue(), "expected ingress rule allowing kube-system on port 8080")

		By("egress allows DNS to kube-dns on port 53")
		var foundDNS bool
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil &&
					peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] == "kube-system" {
					foundDNS = true
					ports := rule.Ports
					Expect(ports).To(ContainElement(
						networkingv1.NetworkPolicyPort{
							Protocol: protocolPtr(corev1.ProtocolUDP),
							Port:     portPtr(53),
						},
					))
				}
			}
		}
		Expect(foundDNS).To(BeTrue(), "expected egress rule allowing DNS to kube-dns")
	})

	It("is idempotent", func() {
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureNetworkPolicy(ctx, nsName)).To(Succeed())
		Expect(r.ensureNetworkPolicy(ctx, nsName)).To(Succeed())
	})
})

var _ = Describe("loadRelayConfig", func() {
	AfterEach(func() {
		os.Unsetenv("RELAY_IMAGE")
		os.Unsetenv("RELAY_INGRESS_CLASS")
		os.Unsetenv("RELAY_INGRESS_BASE_PATH")
		os.Unsetenv("RELAY_INGRESS_ANNOTATIONS")
	})

	It("returns error when RELAY_IMAGE is unset", func() {
		_, err := loadRelayConfig()
		Expect(err).To(MatchError(ContainSubstring("RELAY_IMAGE")))
	})

	It("uses /relay/exec as default base path", func() {
		os.Setenv("RELAY_IMAGE", "img:tag")
		cfg, err := loadRelayConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressBasePath).To(Equal("/relay/exec"))
	})

	It("parses multiple annotations from comma-separated string", func() {
		os.Setenv("RELAY_IMAGE", "img:tag")
		os.Setenv("RELAY_INGRESS_ANNOTATIONS", "foo=bar,baz=qux,key=val=with=equals")
		cfg, err := loadRelayConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressAnnotations).To(HaveKeyWithValue("foo", "bar"))
		Expect(cfg.ingressAnnotations).To(HaveKeyWithValue("baz", "qux"))
		Expect(cfg.ingressAnnotations).To(HaveKeyWithValue("key", "val=with=equals"))
	})

	It("skips malformed annotation tokens (no = sign)", func() {
		os.Setenv("RELAY_IMAGE", "img:tag")
		os.Setenv("RELAY_INGRESS_ANNOTATIONS", "good=value,badtoken,=emptykey")
		cfg, err := loadRelayConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressAnnotations).To(HaveLen(1))
		Expect(cfg.ingressAnnotations).To(HaveKeyWithValue("good", "value"))
	})

	It("leaves ingressClass empty when RELAY_INGRESS_CLASS is unset", func() {
		os.Setenv("RELAY_IMAGE", "img:tag")
		cfg, err := loadRelayConfig()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.ingressClass).To(BeEmpty())
	})
})

var _ = Describe("ensureRelayIngress", func() {
	const envName = "ing-test-env"
	const nsName = "rootenv-lab-" + envName
	ctx := context.Background()

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
		DeferCleanup(func() {
			os.Unsetenv("RELAY_INGRESS_CLASS")
			os.Unsetenv("RELAY_INGRESS_ANNOTATIONS")
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		})
	})

	env := &labv1alpha1.LabEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: envName},
		Spec:       labv1alpha1.LabEnvironmentSpec{OwnerId: "usr", LabId: "lab"},
	}

	It("sets ingressClassName when RELAY_INGRESS_CLASS is set", func() {
		os.Setenv("RELAY_INGRESS_CLASS", "traefik")
		cfg := relayConfig{ingressBasePath: "/relay/exec", ingressClass: "traefik"}
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayIngress(ctx, env, nsName, cfg)).To(Succeed())

		var ing networkingv1.Ingress
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
		Expect(ing.Spec.IngressClassName).NotTo(BeNil())
		Expect(*ing.Spec.IngressClassName).To(Equal("traefik"))
	})

	It("leaves IngressClassName nil when RELAY_INGRESS_CLASS is unset", func() {
		cfg := relayConfig{ingressBasePath: "/relay/exec"}
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayIngress(ctx, env, nsName, cfg)).To(Succeed())

		var ing networkingv1.Ingress
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
		Expect(ing.Spec.IngressClassName).To(BeNil())
	})

	It("sets annotations from config", func() {
		cfg := relayConfig{
			ingressBasePath: "/relay/exec",
			ingressAnnotations: map[string]string{
				"traefik.ingress.kubernetes.io/router.entrypoints": "websecure",
			},
		}
		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayIngress(ctx, env, nsName, cfg)).To(Succeed())

		var ing networkingv1.Ingress
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
		Expect(ing.Annotations).To(HaveKeyWithValue(
			"traefik.ingress.kubernetes.io/router.entrypoints", "websecure",
		))
	})
})
