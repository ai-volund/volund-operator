package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

var _ = Describe("SandboxWarmPool Controller", func() {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		ns       *corev1.Namespace
		poolName string
		tmplName string
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "sandbox-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	newTemplate := func() *volundv1.SandboxTemplate {
		tmplName = fmt.Sprintf("test-tmpl-%d", time.Now().UnixNano())
		runAsUser := int64(65534)
		runAsNonRoot := true
		return &volundv1.SandboxTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tmplName,
				Namespace: ns.Name,
			},
			Spec: volundv1.SandboxTemplateSpec{
				RuntimeClassName:  "gvisor",
				Image:             "ghcr.io/ai-volund/sandbox-runtime:v2",
				NetworkPolicyMode: "managed",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
				RunAsUser:    &runAsUser,
				RunAsNonRoot: &runAsNonRoot,
			},
		}
	}

	newPool := func(replicas int32) *volundv1.SandboxWarmPool {
		poolName = fmt.Sprintf("test-sbpool-%d", time.Now().UnixNano())
		return &volundv1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolName,
				Namespace: ns.Name,
			},
			Spec: volundv1.SandboxWarmPoolSpec{
				Replicas:    replicas,
				TemplateRef: tmplName,
			},
		}
	}

	listSandboxPods := func() []corev1.Pod {
		var list corev1.PodList
		err := k8sClient.List(ctx, &list,
			client.InNamespace(ns.Name),
			client.MatchingLabels{
				"volund.io/sandbox-pool":         poolName,
				"app.kubernetes.io/managed-by": "volund-operator",
			},
		)
		if err != nil {
			return nil
		}
		// Filter out pods being deleted.
		var alive []corev1.Pod
		for _, p := range list.Items {
			if p.DeletionTimestamp == nil {
				alive = append(alive, p)
			}
		}
		return alive
	}

	Describe("TestSandboxWarmPool_ScalesUp", func() {
		It("should create sandbox pods up to the desired count", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(3)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 3))
		})
	})

	Describe("TestSandboxWarmPool_ScalesDown", func() {
		It("should delete excess pods when replicas are reduced", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(3)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for pods to be created.
			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 3))

			// Scale down to 1.
			var latest volundv1.SandboxWarmPool
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      poolName,
				Namespace: ns.Name,
			}, &latest)).To(Succeed())
			latest.Spec.Replicas = 1
			Expect(k8sClient.Update(ctx, &latest)).To(Succeed())

			// Eventually only 1 warm pod should remain.
			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(1))
		})
	})

	Describe("TestSandboxWarmPool_PodSpec", func() {
		It("should set RuntimeClassName, image, resources, and security context from template", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(1)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 1))

			pods := listSandboxPods()
			Expect(pods).To(HaveLen(1))
			pod := pods[0]

			// RuntimeClassName
			Expect(pod.Spec.RuntimeClassName).NotTo(BeNil())
			Expect(*pod.Spec.RuntimeClassName).To(Equal("gvisor"))

			// Image
			Expect(pod.Spec.Containers).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Image).To(Equal("ghcr.io/ai-volund/sandbox-runtime:v2"))

			// Resources
			Expect(pod.Spec.Containers[0].Resources.Requests.Cpu().String()).To(Equal("100m"))
			Expect(pod.Spec.Containers[0].Resources.Requests.Memory().String()).To(Equal("128Mi"))
			Expect(pod.Spec.Containers[0].Resources.Limits.Cpu().String()).To(Equal("1"))
			Expect(pod.Spec.Containers[0].Resources.Limits.Memory().String()).To(Equal("512Mi"))

			// Security context
			Expect(pod.Spec.SecurityContext).NotTo(BeNil())
			Expect(pod.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
			Expect(*pod.Spec.SecurityContext.RunAsUser).To(Equal(int64(65534)))
			Expect(pod.Spec.SecurityContext.RunAsNonRoot).NotTo(BeNil())
			Expect(*pod.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())

			// AutomountServiceAccountToken should be false.
			Expect(pod.Spec.AutomountServiceAccountToken).NotTo(BeNil())
			Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())

			// Labels
			Expect(pod.Labels["volund.io/sandbox-pool"]).To(Equal(poolName))
			Expect(pod.Labels["volund.io/sandbox-state"]).To(Equal("warm"))
			Expect(pod.Labels["app.kubernetes.io/managed-by"]).To(Equal("volund-operator"))

			// Owner reference
			Expect(pod.OwnerReferences).To(HaveLen(1))
			Expect(pod.OwnerReferences[0].Kind).To(Equal("SandboxWarmPool"))
			Expect(pod.OwnerReferences[0].Name).To(Equal(poolName))
			Expect(*pod.OwnerReferences[0].Controller).To(BeTrue())
		})
	})

	Describe("TestSandboxWarmPool_NetworkPolicy", func() {
		It("should create default-deny NetworkPolicy with DNS egress when managed", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(1)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			npName := fmt.Sprintf("sandbox-%s-netpol", poolName)

			// Wait for the NetworkPolicy to be created.
			Eventually(func() error {
				var np networkingv1.NetworkPolicy
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      npName,
					Namespace: ns.Name,
				}, &np)
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      npName,
				Namespace: ns.Name,
			}, &np)).To(Succeed())

			// PolicyTypes should include both Ingress and Egress.
			Expect(np.Spec.PolicyTypes).To(ContainElements(
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			))

			// Ingress should be empty (default-deny).
			Expect(np.Spec.Ingress).To(BeEmpty())

			// Egress should have at least one rule for DNS.
			Expect(np.Spec.Egress).To(HaveLen(1))
			dnsRule := np.Spec.Egress[0]
			Expect(dnsRule.Ports).To(HaveLen(2)) // UDP 53 + TCP 53

			// PodSelector should target sandbox pool pods.
			Expect(np.Spec.PodSelector.MatchLabels["volund.io/sandbox-pool"]).To(Equal(poolName))

			// Owner reference.
			Expect(np.OwnerReferences).To(HaveLen(1))
			Expect(np.OwnerReferences[0].Kind).To(Equal("SandboxWarmPool"))
			Expect(np.OwnerReferences[0].Name).To(Equal(poolName))
		})
	})
})

var _ = Describe("SandboxClaim Controller", func() {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		ns        *corev1.Namespace
		poolName  string
		tmplName  string
		claimName string
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "sbclaim-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	newTemplate := func() *volundv1.SandboxTemplate {
		tmplName = fmt.Sprintf("test-tmpl-%d", time.Now().UnixNano())
		return &volundv1.SandboxTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tmplName,
				Namespace: ns.Name,
			},
			Spec: volundv1.SandboxTemplateSpec{
				RuntimeClassName:  "gvisor",
				Image:             "ghcr.io/ai-volund/sandbox-runtime:v2",
				NetworkPolicyMode: "managed",
			},
		}
	}

	newPool := func(replicas int32) *volundv1.SandboxWarmPool {
		poolName = fmt.Sprintf("test-sbpool-%d", time.Now().UnixNano())
		return &volundv1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolName,
				Namespace: ns.Name,
			},
			Spec: volundv1.SandboxWarmPoolSpec{
				Replicas:    replicas,
				TemplateRef: tmplName,
			},
		}
	}

	newClaim := func() *volundv1.SandboxClaim {
		claimName = fmt.Sprintf("test-claim-%d", time.Now().UnixNano())
		return &volundv1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: ns.Name,
			},
			Spec: volundv1.SandboxClaimSpec{
				PoolRef:        poolName,
				ConversationID: "conv-123",
				TenantID:       "tenant-abc",
			},
		}
	}

	listSandboxPods := func() []corev1.Pod {
		var list corev1.PodList
		err := k8sClient.List(ctx, &list,
			client.InNamespace(ns.Name),
			client.MatchingLabels{
				"volund.io/sandbox-pool":         poolName,
				"app.kubernetes.io/managed-by": "volund-operator",
			},
		)
		if err != nil {
			return nil
		}
		var alive []corev1.Pod
		for _, p := range list.Items {
			if p.DeletionTimestamp == nil {
				alive = append(alive, p)
			}
		}
		return alive
	}

	// markPodsRunning simulates pods becoming Running with a PodIP.
	markPodsRunning := func() {
		pods := listSandboxPods()
		for i := range pods {
			pod := &pods[i]
			pod.Status.Phase = corev1.PodRunning
			pod.Status.PodIP = fmt.Sprintf("10.0.0.%d", i+1)
			_ = k8sClient.Status().Update(ctx, pod)
		}
	}

	Describe("TestSandboxClaim_BindsToWarmPod", func() {
		It("should bind a claim to an available warm pod", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(2)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for warm pods.
			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 2))

			// Mark pods as running so they have PodIPs.
			markPodsRunning()

			claim := newClaim()
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Eventually the claim should be Bound.
			Eventually(func() string {
				var c volundv1.SandboxClaim
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      claimName,
					Namespace: ns.Name,
				}, &c); err != nil {
					return ""
				}
				return c.Status.Phase
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("Bound"))

			// Verify claim status fields.
			var c volundv1.SandboxClaim
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      claimName,
				Namespace: ns.Name,
			}, &c)).To(Succeed())
			Expect(c.Status.PodName).NotTo(BeEmpty())
			Expect(c.Status.Endpoint).To(ContainSubstring(":8888"))
			Expect(c.Status.BoundAt).NotTo(BeNil())
			Expect(c.Status.Error).To(BeEmpty())

			// Verify the claimed pod's label was updated.
			var pod corev1.Pod
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      c.Status.PodName,
				Namespace: ns.Name,
			}, &pod)).To(Succeed())
			Expect(pod.Labels["volund.io/sandbox-state"]).To(Equal("claimed"))
			Expect(pod.Labels["volund.io/sandbox-claim"]).To(Equal(claimName))
			Expect(pod.Labels["volund.io/tenant"]).To(Equal("tenant-abc"))
		})
	})

	Describe("TestSandboxClaim_FailsWhenPoolEmpty", func() {
		It("should set Failed status when no warm pods are available", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			// Create pool with 0 replicas.
			pool := newPool(0)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			claim := newClaim()
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Eventually the claim should be Failed.
			Eventually(func() string {
				var c volundv1.SandboxClaim
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      claimName,
					Namespace: ns.Name,
				}, &c); err != nil {
					return ""
				}
				return c.Status.Phase
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("Failed"))

			// Verify error message.
			var c volundv1.SandboxClaim
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      claimName,
				Namespace: ns.Name,
			}, &c)).To(Succeed())
			Expect(c.Status.Error).To(ContainSubstring("no warm pods available"))
		})
	})

	Describe("TestSandboxClaim_ReleasesOnDelete", func() {
		It("should delete the bound pod when the claim is deleted", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(1)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for warm pod.
			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 1))

			markPodsRunning()

			claim := newClaim()
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Bound.
			Eventually(func() string {
				var c volundv1.SandboxClaim
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      claimName,
					Namespace: ns.Name,
				}, &c); err != nil {
					return ""
				}
				return c.Status.Phase
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("Bound"))

			// Get the bound pod name.
			var c volundv1.SandboxClaim
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      claimName,
				Namespace: ns.Name,
			}, &c)).To(Succeed())
			boundPodName := c.Status.PodName
			Expect(boundPodName).NotTo(BeEmpty())

			// Delete the claim.
			Expect(k8sClient.Delete(ctx, &c)).To(Succeed())

			// Eventually the claim should be gone.
			Eventually(func() bool {
				var claim volundv1.SandboxClaim
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      claimName,
					Namespace: ns.Name,
				}, &claim)
				return errors.IsNotFound(err)
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())

			// The bound pod should be deleted (released).
			Eventually(func() bool {
				var pod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      boundPodName,
					Namespace: ns.Name,
				}, &pod)
				return errors.IsNotFound(err)
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())
		})
	})

	Describe("TestSandboxClaim_TTLExpiry", func() {
		It("should auto-release a claim after TTL expires", func() {
			tmpl := newTemplate()
			Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())

			pool := newPool(1)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			Eventually(func() int {
				return len(listSandboxPods())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 1))

			markPodsRunning()

			// Create a claim with a very short TTL (2 seconds).
			claimName = fmt.Sprintf("test-claim-ttl-%d", time.Now().UnixNano())
			ttl := int32(2)
			claim := &volundv1.SandboxClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      claimName,
					Namespace: ns.Name,
				},
				Spec: volundv1.SandboxClaimSpec{
					PoolRef:        poolName,
					ConversationID: "conv-ttl",
					TenantID:       "tenant-abc",
					TTLSeconds:     &ttl,
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())

			// Wait for Bound.
			Eventually(func() string {
				var c volundv1.SandboxClaim
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      claimName,
					Namespace: ns.Name,
				}, &c); err != nil {
					return ""
				}
				return c.Status.Phase
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("Bound"))

			// After TTL expiry, the claim should be deleted or Released.
			Eventually(func() bool {
				var c volundv1.SandboxClaim
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      claimName,
					Namespace: ns.Name,
				}, &c)
				if errors.IsNotFound(err) {
					return true // claim was deleted
				}
				if err != nil {
					return false
				}
				return c.Status.Phase == "Released"
			}).WithTimeout(30 * time.Second).WithPolling(interval).Should(BeTrue())
		})
	})
})
