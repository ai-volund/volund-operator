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

var _ = Describe("AgentInstance Controller", func() {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		ns       *corev1.Namespace
		instName string
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "inst-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	// No AfterEach namespace cleanup — envtest teardown handles it.

	newInstance := func() *volundv1.AgentInstance {
		instName = fmt.Sprintf("test-inst-%d", time.Now().UnixNano())
		return &volundv1.AgentInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instName,
				Namespace: ns.Name,
				Labels: map[string]string{
					"volund.ai/pool":   "test-pool",
					"volund.ai/tenant": "tenant-abc",
				},
			},
			Spec: volundv1.AgentInstanceSpec{
				PoolName:      "test-pool",
				TenantID:      "tenant-abc",
				Image:         "ghcr.io/ai-volund/volund-agent:latest",
				LLMRouterAddr: "volund-controlplane:9091",
			},
		}
	}

	listPodsForInstance := func(name string) []corev1.Pod {
		var pods corev1.PodList
		err := k8sClient.List(ctx, &pods,
			client.InNamespace(ns.Name),
			client.MatchingLabels{"volund.ai/instance": name},
		)
		if err != nil {
			return nil
		}
		return pods.Items
	}

	Describe("Creating an AgentInstance", func() {
		It("should create a Pod and set state to pending", func() {
			inst := newInstance()
			Expect(k8sClient.Create(ctx, inst)).To(Succeed())

			// The controller should create a Pod for this instance.
			Eventually(func() int {
				return len(listPodsForInstance(instName))
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(1))

			// Verify pod labels.
			pods := listPodsForInstance(instName)
			Expect(pods[0].Labels["volund.ai/pool"]).To(Equal("test-pool"))
			Expect(pods[0].Labels["volund.ai/instance"]).To(Equal(instName))
			Expect(pods[0].Labels["volund.ai/tenant"]).To(Equal("tenant-abc"))

			// The instance should be in pending state with a pod name set.
			Eventually(func() string {
				var fetched volundv1.AgentInstance
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      instName,
					Namespace: ns.Name,
				}, &fetched); err != nil {
					return ""
				}
				return fetched.Status.State
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("pending"))

			var fetched volundv1.AgentInstance
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      instName,
				Namespace: ns.Name,
			}, &fetched)).To(Succeed())
			Expect(fetched.Status.PodName).NotTo(BeEmpty())
		})
	})

	Describe("Transition to warm when pod is running", func() {
		It("should set state to warm when pod phase becomes Running", func() {
			inst := newInstance()
			Expect(k8sClient.Create(ctx, inst)).To(Succeed())

			// Wait for pod creation.
			Eventually(func() int {
				return len(listPodsForInstance(instName))
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(1))

			// Simulate pod becoming Running by updating its status.
			pods := listPodsForInstance(instName)
			pods[0].Status.Phase = corev1.PodRunning
			Expect(k8sClient.Status().Update(ctx, &pods[0])).To(Succeed())

			// The controller should transition the instance to warm.
			Eventually(func() string {
				var fetched volundv1.AgentInstance
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      instName,
					Namespace: ns.Name,
				}, &fetched); err != nil {
					return ""
				}
				return fetched.Status.State
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("warm"))
		})
	})

	Describe("Pod failure handling", func() {
		It("should eventually clean up the instance when pod fails", func() {
			inst := newInstance()
			Expect(k8sClient.Create(ctx, inst)).To(Succeed())

			// Wait for pod and set it to Running first.
			Eventually(func() int {
				return len(listPodsForInstance(instName))
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(1))

			pods := listPodsForInstance(instName)
			pods[0].Status.Phase = corev1.PodRunning
			Expect(k8sClient.Status().Update(ctx, &pods[0])).To(Succeed())

			// Wait for the instance to become warm.
			Eventually(func() string {
				var fetched volundv1.AgentInstance
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      instName,
					Namespace: ns.Name,
				}, &fetched); err != nil {
					return ""
				}
				return fetched.Status.State
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal("warm"))

			// Simulate pod failure.
			pods = listPodsForInstance(instName)
			pods[0].Status.Phase = corev1.PodFailed
			Expect(k8sClient.Status().Update(ctx, &pods[0])).To(Succeed())

			// The controller transitions through terminating and deletes
			// both the pod and the instance. We verify the instance is
			// eventually gone (the terminating state is transient).
			Eventually(func() bool {
				var fetched volundv1.AgentInstance
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      instName,
					Namespace: ns.Name,
				}, &fetched)
				if errors.IsNotFound(err) {
					return true
				}
				// Also accept terminating as progress toward cleanup.
				return fetched.Status.State == "terminating"
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())
		})
	})

	Describe("Terminating state cleanup", func() {
		It("should delete the pod and then the instance", func() {
			inst := newInstance()
			Expect(k8sClient.Create(ctx, inst)).To(Succeed())

			// Wait for pod creation.
			Eventually(func() int {
				return len(listPodsForInstance(instName))
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(1))

			// Manually set instance to terminating.
			Eventually(func() error {
				var fetched volundv1.AgentInstance
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      instName,
					Namespace: ns.Name,
				}, &fetched); err != nil {
					return err
				}
				fetched.Status.State = "terminating"
				return k8sClient.Status().Update(ctx, &fetched)
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			// The controller should delete the pod.
			Eventually(func() int {
				return len(listPodsForInstance(instName))
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(0))

			// Then the instance itself should be deleted.
			Eventually(func() bool {
				var fetched volundv1.AgentInstance
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      instName,
					Namespace: ns.Name,
				}, &fetched)
				return errors.IsNotFound(err)
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())
		})
	})

	Describe("Pod labels and owner references", func() {
		It("should have labels matching the instance and correct owner ref", func() {
			inst := newInstance()
			Expect(k8sClient.Create(ctx, inst)).To(Succeed())

			Eventually(func() int {
				return len(listPodsForInstance(instName))
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(1))

			pods := listPodsForInstance(instName)
			pod := pods[0]
			Expect(pod.Labels["volund.ai/pool"]).To(Equal("test-pool"))
			Expect(pod.Labels["volund.ai/instance"]).To(Equal(instName))
			Expect(pod.Labels["volund.ai/tenant"]).To(Equal("tenant-abc"))

			// Pod should be owned by the instance.
			Expect(pod.OwnerReferences).To(HaveLen(1))
			Expect(pod.OwnerReferences[0].Kind).To(Equal("AgentInstance"))
			Expect(pod.OwnerReferences[0].Name).To(Equal(instName))
		})
	})
})
