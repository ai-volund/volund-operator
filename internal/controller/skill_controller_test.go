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

var _ = Describe("Skill Controller", func() {
	const (
		timeout  = 30 * time.Second
		interval = 250 * time.Millisecond
	)

	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "skill-test-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	getReadyCondition := func(name string) *metav1.Condition {
		var skill volundv1.Skill
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: ns.Name,
		}, &skill); err != nil {
			return nil
		}
		for _, c := range skill.Status.Conditions {
			if c.Type == "Ready" {
				return &c
			}
		}
		return nil
	}

	Describe("Prompt-type skill", func() {
		It("should set Ready=True when prompt is provided", func() {
			name := fmt.Sprintf("prompt-skill-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "prompt",
					Version:     "1.0.0",
					Description: "A test prompt skill",
					Author:      "volund",
					Tags:        []string{"test"},
					Prompt:      "# Test Skill\nYou can do things.",
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Valid"))
		})

		It("should set Ready=False when prompt is missing", func() {
			name := fmt.Sprintf("no-prompt-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "prompt",
					Version:     "1.0.0",
					Description: "Missing prompt content",
					// Prompt intentionally omitted.
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("ValidationFailed"))
			Expect(cond.Message).To(ContainSubstring("prompt is required"))
		})
	})

	Describe("MCP-type skill", func() {
		It("should set Ready=True when runtime.image is provided", func() {
			name := fmt.Sprintf("mcp-skill-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "mcp",
					Version:     "1.0.0",
					Description: "A test MCP skill",
					Runtime: &volundv1.SkillRuntime{
						Image:     "ghcr.io/ai-volund/skill-echo:1.0.0",
						Mode:      "sidecar",
						Transport: "stdio",
						Resources: &volundv1.SkillResources{
							CPU:    "100m",
							Memory: "128Mi",
						},
					},
					Parameters: []volundv1.SkillParameter{
						{
							Name:        "message",
							Type:        "string",
							Description: "The message to echo",
							Required:    true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Valid"))
		})

		It("should set Ready=False when runtime is missing", func() {
			name := fmt.Sprintf("mcp-no-runtime-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "mcp",
					Version:     "1.0.0",
					Description: "MCP skill without runtime",
					// Runtime intentionally omitted.
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("runtime.image is required"))
		})

		It("should set Ready=True with auth configuration", func() {
			name := fmt.Sprintf("mcp-auth-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "mcp",
					Version:     "2.0.0",
					Description: "MCP skill with OAuth",
					Author:      "volund",
					Runtime: &volundv1.SkillRuntime{
						Image:     "ghcr.io/ai-volund/skill-github:2.0.0",
						Mode:      "sidecar",
						Transport: "stdio",
						Auth: &volundv1.SkillAuth{
							Type:     "oauth2",
							Provider: "github",
							Scopes:   []string{"repo", "read:org"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Describe("CLI-type skill", func() {
		It("should set Ready=True when binary and commands are provided", func() {
			name := fmt.Sprintf("cli-skill-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "cli",
					Version:     "1.0.0",
					Description: "GitHub CLI wrapper",
					CLI: &volundv1.SkillCLI{
						Binary:          "gh",
						AllowedCommands: []string{"pr view", "pr list", "issue list"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("Valid"))
		})

		It("should set Ready=False when binary is missing", func() {
			name := fmt.Sprintf("cli-no-binary-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "cli",
					Version:     "1.0.0",
					Description: "CLI skill without binary",
					CLI: &volundv1.SkillCLI{
						AllowedCommands: []string{"pr view"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("cli.binary is required"))
		})

		It("should set Ready=False when allowedCommands is empty", func() {
			name := fmt.Sprintf("cli-no-cmds-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "cli",
					Version:     "1.0.0",
					Description: "CLI skill without commands",
					CLI: &volundv1.SkillCLI{
						Binary:          "gh",
						AllowedCommands: []string{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getReadyCondition(name)
			}).WithTimeout(timeout).WithPolling(interval).ShouldNot(BeNil())

			cond := getReadyCondition(name)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring("allowedCommands must not be empty"))
		})
	})

	Describe("CRD enum validation", func() {
		It("should reject invalid skill type at API level", func() {
			name := fmt.Sprintf("bad-type-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "invalid-type",
					Version:     "1.0.0",
					Description: "Bad type skill",
				},
			}
			err := k8sClient.Create(ctx, skill)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unsupported value"))
		})
	})

	Describe("Updating a skill to fix validation", func() {
		It("should transition Ready from False to True", func() {
			name := fmt.Sprintf("fix-skill-%d", time.Now().UnixNano())
			skill := &volundv1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ns.Name,
				},
				Spec: volundv1.SkillSpec{
					Type:        "prompt",
					Version:     "1.0.0",
					Description: "Broken prompt skill",
					// Prompt intentionally omitted.
				},
			}
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			Eventually(func() metav1.ConditionStatus {
				cond := getReadyCondition(name)
				if cond == nil {
					return ""
				}
				return cond.Status
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(metav1.ConditionFalse))

			// Fix by adding prompt content.
			var latest volundv1.Skill
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: ns.Name,
			}, &latest)).To(Succeed())
			latest.Spec.Prompt = "# Fixed\nNow I have content."
			Expect(k8sClient.Update(ctx, &latest)).To(Succeed())

			Eventually(func() metav1.ConditionStatus {
				cond := getReadyCondition(name)
				if cond == nil {
					return ""
				}
				return cond.Status
			}).WithTimeout(timeout).WithPolling(interval).Should(Equal(metav1.ConditionTrue))

			cond := getReadyCondition(name)
			Expect(cond.Reason).To(Equal("Valid"))
		})
	})
})
