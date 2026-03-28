package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

var _ = Describe("AgentWarmPool Controller", func() {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		ns       *corev1.Namespace
		poolName string
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "pool-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	// No AfterEach namespace cleanup — envtest teardown handles it.
	// Deleting namespaces mid-suite causes "namespace is being terminated"
	// errors for controllers still reconciling in background.

	newPool := func(replicas int32) *volundv1.AgentWarmPool {
		poolName = fmt.Sprintf("test-pool-%d", time.Now().UnixNano())
		return &volundv1.AgentWarmPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolName,
				Namespace: ns.Name,
			},
			Spec: volundv1.AgentWarmPoolSpec{
				Replicas:           replicas,
				TenantID:           "tenant-abc",
				Image:              "ghcr.io/ai-volund/volund-agent:latest",
				LLMRouterAddr:      "volund-controlplane:9091",
				IdleTimeoutSeconds: 300,
			},
		}
	}

	listInstances := func() []volundv1.AgentInstance {
		var list volundv1.AgentInstanceList
		err := k8sClient.List(ctx, &list,
			client.InNamespace(ns.Name),
			client.MatchingLabels{"volund.ai/pool": poolName},
		)
		if err != nil {
			return nil
		}
		return list.Items
	}

	// markInstancesWarm simulates pods becoming Running and triggers the
	// AgentInstance controller to transition instances to "warm". This is
	// needed because the pool controller only counts warm instances.
	markInstancesWarm := func() {
		instances := listInstances()
		for i := range instances {
			inst := &instances[i]
			// Find pods for this instance and set them Running.
			var pods corev1.PodList
			if err := k8sClient.List(ctx, &pods,
				client.InNamespace(ns.Name),
				client.MatchingLabels{"volund.ai/instance": inst.Name},
			); err == nil {
				for j := range pods.Items {
					pods.Items[j].Status.Phase = corev1.PodRunning
					_ = k8sClient.Status().Update(ctx, &pods.Items[j])
				}
			}
		}
	}

	Describe("Creating an AgentWarmPool", func() {
		It("should create AgentInstance objects up to desired count", func() {
			pool := newPool(2)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// The controller counts pending + warm as supply, and caps
			// creation at 2 per reconcile. With desired=2 it creates
			// exactly 2 on the first reconcile.
			Eventually(func() int {
				return len(listInstances())
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(2))

			// Verify instance spec fields.
			instances := listInstances()
			for _, inst := range instances {
				Expect(inst.Spec.PoolName).To(Equal(poolName))
				Expect(inst.Spec.TenantID).To(Equal("tenant-abc"))
				Expect(inst.Spec.Image).To(Equal("ghcr.io/ai-volund/volund-agent:latest"))
				Expect(inst.Spec.LLMRouterAddr).To(Equal("volund-controlplane:9091"))
			}
		})

		It("should not create more instances than desired (no runaway)", func() {
			pool := newPool(2)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for initial creation.
			Eventually(func() int {
				return len(listInstances())
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(2))

			// Wait a few reconcile cycles and verify we still have exactly 2.
			// Pending instances count toward supply, so no extras are created.
			Consistently(func() int {
				return len(listInstances())
			}).WithTimeout(3 * time.Second).WithPolling(interval).Should(Equal(2))
		})
	})

	Describe("Scaling down replicas", func() {
		It("should mark excess warm instances as terminating", func() {
			pool := newPool(3)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for instances to be created.
			Eventually(func() int {
				return len(listInstances())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 3))

			// Mark all current instances as warm (simulating pods running)
			// so the pool controller stabilizes.
			Eventually(func() int {
				markInstancesWarm()
				warmCount := 0
				for _, inst := range listInstances() {
					if inst.Status.State == "warm" {
						warmCount++
					}
				}
				return warmCount
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 3))

			// Scale down to 1.
			var latest volundv1.AgentWarmPool
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      poolName,
				Namespace: ns.Name,
			}, &latest)).To(Succeed())
			latest.Spec.Replicas = 1
			Expect(k8sClient.Update(ctx, &latest)).To(Succeed())

			// Eventually some instances should be marked terminating.
			Eventually(func() int {
				count := 0
				for _, inst := range listInstances() {
					if inst.Status.State == "terminating" {
						count++
					}
				}
				return count
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 1))
		})
	})

	Describe("Pool status", func() {
		It("should reflect correct ready count after instances become warm", func() {
			pool := newPool(2)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Wait for instances.
			Eventually(func() int {
				return len(listInstances())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 2))

			// Mark instances warm.
			Eventually(func() int {
				markInstancesWarm()
				warmCount := 0
				for _, inst := range listInstances() {
					if inst.Status.State == "warm" {
						warmCount++
					}
				}
				return warmCount
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 2))

			// The pool controller should update ReadyReplicas.
			Eventually(func() int32 {
				var p volundv1.AgentWarmPool
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      poolName,
					Namespace: ns.Name,
				}, &p); err != nil {
					return -1
				}
				return p.Status.ReadyReplicas
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", int32(2)))
		})
	})

	Describe("Owner references", func() {
		It("should set owner reference on created instances", func() {
			pool := newPool(1)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			Eventually(func() int {
				return len(listInstances())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 1))

			inst := listInstances()[0]
			Expect(inst.OwnerReferences).To(HaveLen(1))
			Expect(inst.OwnerReferences[0].Kind).To(Equal("AgentWarmPool"))
			Expect(inst.OwnerReferences[0].Name).To(Equal(poolName))
			Expect(*inst.OwnerReferences[0].Controller).To(BeTrue())
		})
	})

	Describe("Pool deletion", func() {
		It("should delete the pool; instances have owner refs for cascade", func() {
			pool := newPool(2)
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			Eventually(func() int {
				return len(listInstances())
			}).WithTimeout(timeout).WithPolling(interval).Should(BeNumerically(">=", 2))

			// Verify all instances have owner references pointing to the pool.
			// In a real cluster, K8s garbage collector cascades the delete.
			// envtest does not run the GC, so we only verify the refs are set.
			instances := listInstances()
			for _, inst := range instances {
				Expect(inst.OwnerReferences).To(HaveLen(1))
				Expect(inst.OwnerReferences[0].Kind).To(Equal("AgentWarmPool"))
				Expect(inst.OwnerReferences[0].Name).To(Equal(poolName))
			}

			// Delete the pool itself.
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())

			Eventually(func() bool {
				var p volundv1.AgentWarmPool
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      poolName,
					Namespace: ns.Name,
				}, &p)
				return errors.IsNotFound(err)
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())
		})
	})
})
