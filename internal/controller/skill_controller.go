package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

// SkillReconciler reconciles Skill objects.
// Skill is primarily a data resource — the reconciler validates the spec
// and sets a Ready condition. Type-specific fields are checked for consistency
// (e.g., prompt-type skills must have a prompt, mcp-type must have runtime).
type SkillReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=skills,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=skills/status,verbs=get;update;patch

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

	setCondition(&skill.Status.Conditions, condition)
	if err := r.Status().Update(ctx, &skill); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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
		Complete(r)
}
