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

// AgentProfileReconciler reconciles AgentProfile objects.
// AgentProfile is primarily a data resource — the reconciler validates
// the profile and sets a Ready condition.
type AgentProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=agentprofiles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=agentprofiles/status,verbs=get;update;patch

func (r *AgentProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var profile volundv1.AgentProfile
	if err := r.Get(ctx, req.NamespacedName, &profile); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling agent profile",
		"name", profile.Name,
		"profileType", profile.Spec.ProfileType,
		"provider", profile.Spec.Model.Provider,
		"model", profile.Spec.Model.Name,
	)

	// Validate the profile and compute the Ready condition.
	condition := r.validate(&profile)

	setCondition(&profile.Status.Conditions, condition)
	if err := r.Status().Update(ctx, &profile); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// validate checks the AgentProfile spec and returns a Ready condition.
func (r *AgentProfileReconciler) validate(profile *volundv1.AgentProfile) metav1.Condition {
	spec := &profile.Spec

	if spec.DisplayName == "" {
		return metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             "ValidationFailed",
			Message:            "displayName is required",
		}
	}

	if spec.SystemPrompt == "" {
		return metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             "ValidationFailed",
			Message:            "systemPrompt is required",
		}
	}

	if spec.Model.Provider == "" || spec.Model.Name == "" {
		return metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             "ValidationFailed",
			Message:            "model.provider and model.name are required",
		}
	}

	if spec.ProfileType != "orchestrator" && spec.ProfileType != "specialist" {
		return metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             "ValidationFailed",
			Message:            fmt.Sprintf("profileType must be orchestrator or specialist, got %q", spec.ProfileType),
		}
	}

	// Validate visibility field.
	if spec.Visibility != "" && spec.Visibility != "system" && spec.Visibility != "user" {
		return metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             "ValidationFailed",
			Message:            fmt.Sprintf("visibility must be system or user, got %q", spec.Visibility),
		}
	}

	// User-scoped profiles must have an ownerID.
	if spec.Visibility == "user" && spec.OwnerID == "" {
		return metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             "ValidationFailed",
			Message:            "ownerId is required when visibility is user",
		}
	}

	return metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: profile.Generation,
		Reason:             "Valid",
		Message:            "profile validated successfully",
	}
}

// SetupWithManager registers the controller with the manager.
func (r *AgentProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&volundv1.AgentProfile{}).
		Complete(r)
}
