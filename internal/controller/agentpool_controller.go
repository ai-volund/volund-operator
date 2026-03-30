package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

// AgentWarmPoolReconciler reconciles AgentWarmPool objects.
type AgentWarmPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=agentwarmpool,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=agentwarmpool/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=volund.ai,resources=agentinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

func (r *AgentWarmPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pool volundv1.AgentWarmPool
	if err := r.Get(ctx, req.NamespacedName, &pool); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// List all AgentInstances owned by this pool.
	var instances volundv1.AgentInstanceList
	if err := r.List(ctx, &instances,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{"volund.ai/pool": pool.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list instances: %w", err)
	}

	// Partition instances by state.
	var pending, warm, active, terminating int32
	for _, inst := range instances.Items {
		switch inst.Status.State {
		case "", "pending":
			pending++
		case "warm":
			warm++
		case "active":
			active++
		case "terminating":
			terminating++
		}
	}

	desired := pool.Spec.Replicas

	// Count pending + warm as "supply" — pending instances are in-flight
	// toward becoming warm. Without this, the controller creates unbounded
	// instances while existing ones are still starting up.
	supply := pending + warm

	logger.Info("reconciling pool",
		"pending", pending, "warm", warm, "active", active,
		"terminating", terminating, "supply", supply, "desired", desired)

	// Scale up: create new instances only if supply is below desired.
	// Cap new creations at 2 per reconcile to avoid bursts.
	if supply < desired {
		toCreate := desired - supply
		const maxBurst int32 = 2
		if toCreate > maxBurst {
			toCreate = maxBurst
		}
		for i := int32(0); i < toCreate; i++ {
			if err := r.createInstance(ctx, &pool); err != nil {
				return ctrl.Result{}, fmt.Errorf("create instance: %w", err)
			}
		}
		// If we still need more, requeue after a brief delay to avoid bursts.
		if supply+toCreate < desired {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	// Scale down: mark excess warm instances as terminating.
	// Only terminate warm (idle) instances, never pending or active.
	if warm > desired {
		excess := warm - desired
		deleted := int32(0)
		for i := range instances.Items {
			if deleted >= excess {
				break
			}
			inst := &instances.Items[i]
			if inst.Status.State != "warm" {
				continue
			}
			inst.Status.State = "terminating"
			if err := r.Status().Update(ctx, inst); err != nil {
				logger.Error(err, "mark terminating", "instance", inst.Name)
			}
			deleted++
		}
	}

	// Update pool status.
	pool.Status.ReadyReplicas = warm
	pool.Status.ActiveReplicas = active
	setCondition(&pool.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: pool.Generation,
		Reason:             "Reconciled",
		Message:            fmt.Sprintf("%d pending, %d warm, %d active", pending, warm, active),
	})
	if err := r.Status().Update(ctx, &pool); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	// If there are pending instances, requeue to check their progress.
	if pending > 0 {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *AgentWarmPoolReconciler) createInstance(ctx context.Context, pool *volundv1.AgentWarmPool) error {
	inst := &volundv1.AgentInstance{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				"volund.ai/pool":   pool.Name,
				"volund.ai/tenant": pool.Spec.TenantID,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(pool, volundv1.GroupVersion.WithKind("AgentWarmPool")),
			},
		},
		Spec: volundv1.AgentInstanceSpec{
			PoolName:      pool.Name,
			TenantID:      pool.Spec.TenantID,
			ProfileID:     pool.Spec.ProfileID,
			Image:         pool.Spec.Image,
			LLMRouterAddr: pool.Spec.LLMRouterAddr,
			NATSUrl:       pool.Spec.NATSUrl,
		},
	}
	return r.Create(ctx, inst)
}

// SetupWithManager registers the controller with the manager.
func (r *AgentWarmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&volundv1.AgentWarmPool{}).
		Owns(&volundv1.AgentInstance{}).
		Complete(r)
}

// setCondition upserts a condition into the slice.
func setCondition(conditions *[]metav1.Condition, c metav1.Condition) {
	c.LastTransitionTime = metav1.Now()
	for i, existing := range *conditions {
		if existing.Type == c.Type {
			if existing.Status != c.Status {
				(*conditions)[i] = c
			}
			return
		}
	}
	*conditions = append(*conditions, c)
}

// podForInstance builds a Pod spec for the given AgentInstance.
// If profile is non-nil, the pod receives the profile's type, model, and system
// prompt as environment variables so the agent starts with the correct role.
// skills are sidecar-mode Skill CRDs that need their binaries injected.
func podForInstance(inst *volundv1.AgentInstance, profile *volundv1.AgentProfile, skills []volundv1.Skill) *corev1.Pod {
	env := []corev1.EnvVar{
		// VOLUND_INSTANCE_ID is injected via Downward API so each pod
		// reports its own unique name in agent_start stream events.
		{
			Name: "VOLUND_INSTANCE_ID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{Name: "VOLUND_TENANT_ID", Value: inst.Spec.TenantID},
		// VOLUND_PROFILE is the NATS pool subject suffix — must match
		// the profileName the gateway dispatches to.
		{Name: "VOLUND_PROFILE", Value: inst.Spec.ProfileID},
		{Name: "VOLUND_LLM_ROUTER_ADDR", Value: inst.Spec.LLMRouterAddr},
		{Name: "VOLUND_NATS_URL", Value: inst.Spec.NATSUrl},
		// Redis for session memory.
		{Name: "VOLUND_REDIS_ADDR", Value: "redis:6379"},
	}

	// Inject profile fields when the AgentProfile CR is available.
	if profile != nil {
		env = append(env,
			corev1.EnvVar{Name: "VOLUND_PROFILE_TYPE", Value: profile.Spec.ProfileType},
			corev1.EnvVar{Name: "VOLUND_PROVIDER", Value: profile.Spec.Model.Provider},
			corev1.EnvVar{Name: "VOLUND_MODEL", Value: profile.Spec.Model.Name},
		)
		if profile.Spec.SystemPrompt != "" {
			env = append(env, corev1.EnvVar{Name: "VOLUND_SYSTEM_PROMPT", Value: profile.Spec.SystemPrompt})
		}
		if profile.Spec.Model.MaxTokens > 0 {
			env = append(env, corev1.EnvVar{Name: "VOLUND_MAX_TOKENS", Value: fmt.Sprintf("%d", profile.Spec.Model.MaxTokens)})
		}
		if profile.Spec.MaxToolRounds > 0 {
			env = append(env, corev1.EnvVar{Name: "VOLUND_MAX_TOOL_ROUNDS", Value: fmt.Sprintf("%d", profile.Spec.MaxToolRounds)})
		}
	}

	// Build init containers for sidecar-mode skills.
	// Each skill's container image is run as an init container that copies
	// its MCP binary into /skills-bin (shared emptyDir volume).
	// Convention: the skill image has the binary at /usr/local/bin/mcp-{name}.
	var initContainers []corev1.Container
	needsSkillVolume := false
	for _, sk := range skills {
		if sk.Spec.Type != "mcp" || sk.Spec.Runtime == nil || sk.Spec.Runtime.Mode != "sidecar" {
			continue
		}
		name := sk.Name
		image := sk.Spec.Runtime.Image
		if image == "" {
			continue
		}
		needsSkillVolume = true
		initContainers = append(initContainers, corev1.Container{
			Name:            fmt.Sprintf("skill-%s", name),
			Image:           image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"cp", fmt.Sprintf("/usr/local/bin/mcp-%s", name), "/skills-bin/"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "skills-bin", MountPath: "/skills-bin"},
			},
		})
	}

	// Volumes and volume mounts for skills.
	var volumes []corev1.Volume
	var agentVolumeMounts []corev1.VolumeMount
	if needsSkillVolume {
		volumes = append(volumes, corev1.Volume{
			Name: "skills-bin",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      "skills-bin",
			MountPath: "/skills-bin",
		})
		// Add /skills-bin to PATH so the agent can find mcp-{name} binaries.
		env = append(env, corev1.EnvVar{
			Name:  "PATH",
			Value: "/skills-bin:/usr/local/bin:/usr/bin:/bin",
		})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: inst.Name + "-",
			Namespace:    inst.Namespace,
			Labels: map[string]string{
				"volund.ai/pool":     inst.Spec.PoolName,
				"volund.ai/instance": inst.Name,
				"volund.ai/tenant":   inst.Spec.TenantID,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(inst, volundv1.GroupVersion.WithKind("AgentInstance")),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:  corev1.RestartPolicyNever,
			InitContainers: initContainers,
			Containers: []corev1.Container{
				{
					Name:            "agent",
					Image:           inst.Spec.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env:             env,
					VolumeMounts:    agentVolumeMounts,
				},
			},
			Volumes: volumes,
		},
	}
}
