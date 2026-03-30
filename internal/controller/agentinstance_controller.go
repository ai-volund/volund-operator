package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

// AgentInstanceReconciler reconciles AgentInstance objects.
type AgentInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=agentinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=agentinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=volund.ai,resources=agentprofiles,verbs=get;list;watch
// +kubebuilder:rbac:groups=volund.ai,resources=skills,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

func (r *AgentInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var inst volundv1.AgentInstance
	if err := r.Get(ctx, req.NamespacedName, &inst); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Find the Pod owned by this instance.
	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{"volund.ai/instance": inst.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list pods: %w", err)
	}

	switch inst.Status.State {
	case "", "pending":
		return r.reconcilePending(ctx, &inst, &pods)
	case "warm", "active":
		return r.reconcileRunning(ctx, &inst, &pods)
	case "terminating":
		return r.reconcileTerminating(ctx, &inst, &pods)
	}

	return ctrl.Result{}, nil
}

// reconcilePending creates the agent Pod if it doesn't exist yet.
func (r *AgentInstanceReconciler) reconcilePending(ctx context.Context, inst *volundv1.AgentInstance, pods *corev1.PodList) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if len(pods.Items) == 0 {
		// Look up the AgentProfile CR to inject profile type and model config.
		var profile *volundv1.AgentProfile
		if inst.Spec.ProfileID != "" {
			var p volundv1.AgentProfile
			key := client.ObjectKey{Name: inst.Spec.ProfileID, Namespace: inst.Namespace}
			if err := r.Get(ctx, key, &p); err != nil {
				logger.Info("profile not found, creating pod without profile config",
					"profile", inst.Spec.ProfileID, "error", err)
			} else {
				profile = &p
			}
		}

		// List sidecar-mode skills to inject as init containers.
		var skillList volundv1.SkillList
		if err := r.List(ctx, &skillList, client.InNamespace(inst.Namespace)); err != nil {
			logger.Info("failed to list skills for sidecar injection", "error", err)
		}
		var sidecarSkills []volundv1.Skill
		for _, sk := range skillList.Items {
			if sk.Spec.Type == "mcp" && sk.Spec.Runtime != nil && sk.Spec.Runtime.Mode == "sidecar" {
				sidecarSkills = append(sidecarSkills, sk)
			}
		}

		pod := podForInstance(inst, profile, sidecarSkills)
		if err := r.Create(ctx, pod); err != nil {
			return ctrl.Result{}, fmt.Errorf("create pod: %w", err)
		}
		logger.Info("created pod", "instance", inst.Name, "pod", pod.Name,
			"profile", inst.Spec.ProfileID, "profileFound", profile != nil)
		inst.Status.State = "pending"
		inst.Status.PodName = pod.Name
		if err := r.Status().Update(ctx, inst); err != nil && !errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	pod := &pods.Items[0]
	inst.Status.PodName = pod.Name

	if pod.Status.Phase == corev1.PodRunning {
		inst.Status.State = "warm"
		logger.Info("instance warm", "instance", inst.Name)
	}

	if err := r.Status().Update(ctx, inst); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// reconcileRunning checks if the Pod is still alive; marks failed instances.
func (r *AgentInstanceReconciler) reconcileRunning(ctx context.Context, inst *volundv1.AgentInstance, pods *corev1.PodList) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if len(pods.Items) == 0 {
		// Pod disappeared — recycle the instance.
		logger.Info("pod gone, marking terminating", "instance", inst.Name)
		inst.Status.State = "terminating"
		if err := r.Status().Update(ctx, inst); err != nil && !errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	pod := &pods.Items[0]
	if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		logger.Info("pod finished", "instance", inst.Name, "phase", pod.Status.Phase)
		inst.Status.State = "terminating"
		if err := r.Status().Update(ctx, inst); err != nil && !errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileTerminating deletes the Pod and the AgentInstance itself.
func (r *AgentInstanceReconciler) reconcileTerminating(ctx context.Context, inst *volundv1.AgentInstance, pods *corev1.PodList) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil {
			continue // already deleting
		}
		if err := r.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, fmt.Errorf("delete pod: %w", err)
		}
		logger.Info("deleted pod", "pod", pod.Name)
	}

	// Delete the AgentInstance once the Pod is gone.
	if len(pods.Items) == 0 {
		if err := r.Delete(ctx, inst); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		logger.Info("deleted instance", "instance", inst.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *AgentInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&volundv1.AgentInstance{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
