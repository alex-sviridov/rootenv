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
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
			Expect(os.Setenv("RELAY_EXEC_IMAGE", "relay-primitive:test")).To(Succeed())
			DeferCleanup(func() { Expect(os.Unsetenv("RELAY_EXEC_IMAGE")).To(Succeed()) })

			Expect(os.Setenv("RELAY_GRADER_IMAGE", "relay-grader:test")).To(Succeed())
			DeferCleanup(func() { Expect(os.Unsetenv("RELAY_GRADER_IMAGE")).To(Succeed()) })

			labImagesDir, err := os.MkdirTemp("", "lab-images")
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(labImagesDir, "busybox"), []byte("busybox:test"), 0644)).To(Succeed())
			Expect(os.Setenv("LAB_IMAGES_DIR", labImagesDir)).To(Succeed())
			DeferCleanup(func() {
				Expect(os.Unsetenv("LAB_IMAGES_DIR")).To(Succeed())
				Expect(os.RemoveAll(labImagesDir)).To(Succeed())
			})

			By("creating the custom resource for the Kind LabEnvironment")
			err = k8sClient.Get(ctx, typeNamespacedName, labenvironment)
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

var _ = Describe("ensureNetworkPolicy (denyall)", func() {
	ctx := context.Background()

	It("creates denyall policy with correct shape", func() {
		nsName := "rootenv-lab-denyall-shape"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

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
		nsName := "rootenv-lab-denyall-idempotent"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureNetworkPolicy(ctx, nsName)).To(Succeed())
		Expect(r.ensureNetworkPolicy(ctx, nsName)).To(Succeed())
	})
})
