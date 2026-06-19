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

		By("NetworkPolicy allow-traefik-to-relay exists")
		var np networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "allow-traefik-to-relay"}, &np)).To(Succeed())
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
