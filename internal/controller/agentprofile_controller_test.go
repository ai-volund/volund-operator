package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

var _ = Describe("AgentProfile Controller", func() {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		ns          *corev1.Namespace
		profileName string
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "profile-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	// No AfterEach namespace cleanup — envtest teardown handles it.

	getReadyCondition := func(name string) *metav1.Condition {
		var profile volundv1.AgentProfile
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: ns.Name,
		}, &profile); err != nil {
			return nil
		}
		for _, c := range profile.Status.Conditions {
			if c.Type == "Ready" {
				return &c
			}
		}
		return nil
	}

	Describe("Creating a valid profile", func() {
		It("should set Ready condition to True", func() {
			profileName = fmt.Sprintf("valid-profile-%d", time.Now().UnixNano())
			profile := &volundv1.AgentProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      profileName,
					Namespace: ns.Name,
				},
				Spec: volundv1.AgentProfileSpec{
					DisplayName:  "Test Agent",
					Description:  "A test agent profile",
					ProfileType:  "specialist",
					SystemPrompt: "You are a helpful assistant.",
					Model: volundv1.ModelConfig{
						Provider: "anthropic",
						Name:     "claude-sonnet-4-20250514",
					},
					MaxToolRounds: 25,
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(profileName)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(profileName)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Valid"))
			Expect(cond.Message).To(ContainSubstring("validated successfully"))
		})
	})

	Describe("Creating a profile missing systemPrompt", func() {
		It("should set Ready condition to False with appropriate message", func() {
			profileName = fmt.Sprintf("no-prompt-%d", time.Now().UnixNano())
			profile := &volundv1.AgentProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      profileName,
					Namespace: ns.Name,
				},
				Spec: volundv1.AgentProfileSpec{
					DisplayName: "Missing Prompt Agent",
					ProfileType: "specialist",
					// SystemPrompt intentionally omitted.
					Model: volundv1.ModelConfig{
						Provider: "anthropic",
						Name:     "claude-sonnet-4-20250514",
					},
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(profileName)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(profileName)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ValidationFailed"))
			Expect(cond.Message).To(ContainSubstring("systemPrompt is required"))
		})
	})

	Describe("Creating a profile with invalid profileType", func() {
		It("should be rejected by CRD validation", func() {
			profileName = fmt.Sprintf("bad-type-%d", time.Now().UnixNano())
			profile := &volundv1.AgentProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      profileName,
					Namespace: ns.Name,
				},
				Spec: volundv1.AgentProfileSpec{
					DisplayName:  "Bad Type Agent",
					ProfileType:  "invalid-type",
					SystemPrompt: "You are a helpful assistant.",
					Model: volundv1.ModelConfig{
						Provider: "anthropic",
						Name:     "claude-sonnet-4-20250514",
					},
				},
			}
			err := k8sClient.Create(ctx, profile)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value"))
		})
	})

	Describe("Updating a profile to fix validation", func() {
		It("should transition Ready from False to True", func() {
			profileName = fmt.Sprintf("fix-profile-%d", time.Now().UnixNano())
			profile := &volundv1.AgentProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      profileName,
					Namespace: ns.Name,
				},
				Spec: volundv1.AgentProfileSpec{
					DisplayName: "Broken Agent",
					ProfileType: "specialist",
					// SystemPrompt intentionally omitted.
					Model: volundv1.ModelConfig{
						Provider: "anthropic",
						Name:     "claude-sonnet-4-20250514",
					},
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			// Wait for the validation failure condition.
			Eventually(func() metav1.ConditionStatus {
				cond := getReadyCondition(profileName)
				if cond == nil {
					return ""
				}
				return cond.Status
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(metav1.ConditionFalse))

			// Fix the profile by adding a system prompt.
			var latest volundv1.AgentProfile
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      profileName,
				Namespace: ns.Name,
			}, &latest)).To(Succeed())
			latest.Spec.SystemPrompt = "You are now a valid assistant."
			Expect(k8sClient.Update(ctx, &latest)).To(Succeed())

			// The controller should transition to Ready=True.
			Eventually(func() metav1.ConditionStatus {
				cond := getReadyCondition(profileName)
				if cond == nil {
					return ""
				}
				return cond.Status
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(metav1.ConditionTrue))

			cond := getReadyCondition(profileName)
			Expect(cond.Reason).To(Equal("Valid"))
		})
	})
})
