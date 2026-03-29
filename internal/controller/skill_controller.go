package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

// SkillReconciler reconciles Skill objects.
// For sidecar-mode skills, the reconciler validates the spec and sets a Ready
// condition. For shared-mode skills, it additionally creates a Deployment +
// Service so all agents in the tenant namespace connect via HTTP.
type SkillReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=skills,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=skills/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *SkillReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var skill volundv1.Skill
	if err := r.Get(ctx, req.NamespacedName, &skill); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling skill",
		"name", skill.Name,
		"type", skill.Spec.Type,
		"version", skill.Spec.Version,
	)

	condition := r.validate(&skill)

	// For shared-mode MCP skills, ensure a Deployment + Service exist.
	if condition.Status == metav1.ConditionTrue && isSharedMode(&skill) {
		if err := r.reconcileSharedDeployment(ctx, &skill); err != nil {
			logger.Error(err, "failed to reconcile shared skill deployment")
			condition = metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: skill.Generation,
				Reason:             "DeploymentFailed",
				Message:            err.Error(),
			}
		}
	}

	setCondition(&skill.Status.Conditions, condition)
	if err := r.Status().Update(ctx, &skill); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// isSharedMode returns true if the skill should run as a shared per-tenant Deployment.
func isSharedMode(skill *volundv1.Skill) bool {
	return skill.Spec.Type == "mcp" &&
		skill.Spec.Runtime != nil &&
		skill.Spec.Runtime.Mode == "shared"
}

// sharedSkillName returns the Deployment/Service name for a shared skill.
func sharedSkillName(skill *volundv1.Skill) string {
	return fmt.Sprintf("skill-%s", skill.Name)
}

// reconcileSharedDeployment ensures a Deployment and Service exist for a
// shared-mode skill. The Deployment runs one replica of the skill's container
// image, and the Service exposes it on port 8080 so agents can connect via HTTP.
func (r *SkillReconciler) reconcileSharedDeployment(ctx context.Context, skill *volundv1.Skill) error {
	name := sharedSkillName(skill)
	labels := map[string]string{
		"volund.ai/skill":      skill.Name,
		"volund.ai/skill-mode": "shared",
		"app":                  name,
	}

	// --- Deployment ---
	replicas := int32(1)
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: skill.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(skill, volundv1.GroupVersion.WithKind("Skill")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "skill",
							Image:           skill.Spec.Runtime.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{Name: "MCP_TRANSPORT", Value: "http"},
								{Name: "MCP_HTTP_PORT", Value: "8080"},
							},
						},
					},
				},
			},
		},
	}

	// Apply resource limits if specified.
	if skill.Spec.Runtime.Resources != nil {
		rl := corev1.ResourceList{}
		if skill.Spec.Runtime.Resources.CPU != "" {
			rl[corev1.ResourceCPU] = resource.MustParse(skill.Spec.Runtime.Resources.CPU)
		}
		if skill.Spec.Runtime.Resources.Memory != "" {
			rl[corev1.ResourceMemory] = resource.MustParse(skill.Spec.Runtime.Resources.Memory)
		}
		if len(rl) > 0 {
			desired.Spec.Template.Spec.Containers[0].Resources.Requests = rl
			desired.Spec.Template.Spec.Containers[0].Resources.Limits = rl
		}
	}

	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if errors.IsNotFound(err) {
		log.FromContext(ctx).Info("creating shared skill deployment", "name", name)
		return r.Create(ctx, desired)
	} else if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}

	// Update if the image changed.
	if existing.Spec.Template.Spec.Containers[0].Image != skill.Spec.Runtime.Image {
		existing.Spec.Template.Spec.Containers[0].Image = skill.Spec.Runtime.Image
		log.FromContext(ctx).Info("updating shared skill deployment image",
			"name", name, "image", skill.Spec.Runtime.Image)
		if err := r.Update(ctx, &existing); err != nil {
			return fmt.Errorf("update deployment: %w", err)
		}
	}

	// --- Service ---
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: skill.Namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(skill, volundv1.GroupVersion.WithKind("Skill")),
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt32(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	var existingSvc corev1.Service
	err = r.Get(ctx, client.ObjectKeyFromObject(svc), &existingSvc)
	if errors.IsNotFound(err) {
		log.FromContext(ctx).Info("creating shared skill service", "name", name)
		return r.Create(ctx, svc)
	} else if err != nil {
		return fmt.Errorf("get service: %w", err)
	}

	return nil
}

func (r *SkillReconciler) validate(skill *volundv1.Skill) metav1.Condition {
	spec := &skill.Spec

	if spec.Version == "" {
		return r.invalid(skill, "version is required")
	}

	if spec.Description == "" {
		return r.invalid(skill, "description is required")
	}

	switch spec.Type {
	case "prompt":
		if spec.Prompt == "" {
			return r.invalid(skill, "prompt is required for type=prompt")
		}
	case "mcp":
		if spec.Runtime == nil || spec.Runtime.Image == "" {
			return r.invalid(skill, "runtime.image is required for type=mcp")
		}
	case "cli":
		if spec.CLI == nil || spec.CLI.Binary == "" {
			return r.invalid(skill, "cli.binary is required for type=cli")
		}
		if spec.CLI != nil && len(spec.CLI.AllowedCommands) == 0 {
			return r.invalid(skill, "cli.allowedCommands must not be empty for type=cli")
		}
	default:
		return r.invalid(skill, fmt.Sprintf("type must be prompt, mcp, or cli, got %q", spec.Type))
	}

	return metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: skill.Generation,
		Reason:             "Valid",
		Message:            "skill validated successfully",
	}
}

func (r *SkillReconciler) invalid(skill *volundv1.Skill, msg string) metav1.Condition {
	return metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: skill.Generation,
		Reason:             "ValidationFailed",
		Message:            msg,
	}
}

func (r *SkillReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&volundv1.Skill{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
