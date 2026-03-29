package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

// ── SandboxWarmPoolReconciler ───────────────────────────────────────────────

// SandboxWarmPoolReconciler reconciles SandboxWarmPool objects.
// It watches SandboxWarmPool resources, looks up the referenced
// SandboxTemplate, and creates/deletes sandbox pods to maintain the
// desired warm pool size. It also manages NetworkPolicy resources
// when the template's networkPolicyMode is "managed".
type SandboxWarmPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=sandboxwarmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=sandboxwarmpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=volund.ai,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *SandboxWarmPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pool volundv1.SandboxWarmPool
	if err := r.Get(ctx, req.NamespacedName, &pool); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Look up the referenced SandboxTemplate.
	var tmpl volundv1.SandboxTemplate
	tmplKey := client.ObjectKey{Name: pool.Spec.TemplateRef, Namespace: pool.Namespace}
	if err := r.Get(ctx, tmplKey, &tmpl); err != nil {
		logger.Error(err, "failed to get SandboxTemplate", "templateRef", pool.Spec.TemplateRef)
		pool.Status.Available = 0
		pool.Status.Active = 0
		pool.Status.Total = 0
		_ = r.Status().Update(ctx, &pool)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Ensure NetworkPolicy exists when managed mode is set.
	if tmpl.Spec.NetworkPolicyMode == "" || tmpl.Spec.NetworkPolicyMode == "managed" {
		if err := r.reconcileNetworkPolicy(ctx, &pool, &tmpl); err != nil {
			logger.Error(err, "failed to reconcile NetworkPolicy")
			return ctrl.Result{}, err
		}
	}

	// List all pods owned by this pool.
	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{
			"volund.io/sandbox-pool":         pool.Name,
			"app.kubernetes.io/managed-by": "volund-operator",
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list sandbox pods: %w", err)
	}

	// Partition pods by state label.
	var warm, claimed int32
	var warmPods []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue // skip pods being deleted
		}
		state := pod.Labels["volund.io/sandbox-state"]
		switch state {
		case "claimed":
			claimed++
		default: // "warm" or unlabeled
			warm++
			warmPods = append(warmPods, pod)
		}
	}

	desired := pool.Spec.Replicas

	logger.Info("reconciling sandbox pool",
		"warm", warm, "claimed", claimed, "desired", desired)

	// Scale up: create new sandbox pods if warm count is below desired.
	if warm < desired {
		toCreate := desired - warm
		const maxBurst int32 = 3
		if toCreate > maxBurst {
			toCreate = maxBurst
		}
		for i := int32(0); i < toCreate; i++ {
			if err := r.createSandboxPod(ctx, &pool, &tmpl); err != nil {
				return ctrl.Result{}, fmt.Errorf("create sandbox pod: %w", err)
			}
		}
		if warm+toCreate < desired {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	// Scale down: delete excess warm pods.
	if warm > desired {
		excess := warm - desired
		deleted := int32(0)
		for _, pod := range warmPods {
			if deleted >= excess {
				break
			}
			if err := r.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
				logger.Error(err, "delete excess sandbox pod", "pod", pod.Name)
				continue
			}
			logger.Info("deleted excess sandbox pod", "pod", pod.Name)
			deleted++
		}
	}

	// Update pool status.
	pool.Status.Available = warm
	pool.Status.Active = claimed
	pool.Status.Total = warm + claimed
	if err := r.Status().Update(ctx, &pool); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// createSandboxPod creates a new sandbox pod from the template.
func (r *SandboxWarmPoolReconciler) createSandboxPod(ctx context.Context, pool *volundv1.SandboxWarmPool, tmpl *volundv1.SandboxTemplate) error {
	runtimeClass := tmpl.Spec.RuntimeClassName

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("sandbox-%s-", pool.Name),
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "volund-operator",
				"volund.io/sandbox-pool":         pool.Name,
				"volund.io/sandbox-state":        "warm",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(pool, volundv1.GroupVersion.WithKind("SandboxWarmPool")),
			},
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClass,
			RestartPolicy:    corev1.RestartPolicyNever,
			// Do not mount service account token for security.
			AutomountServiceAccountToken: boolPtr(false),
			Containers: []corev1.Container{
				{
					Name:            "runtime",
					Image:           tmpl.Spec.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{ContainerPort: 8888, Protocol: corev1.ProtocolTCP},
					},
					Resources: tmpl.Spec.Resources,
				},
			},
		},
	}

	// Apply security context from template.
	if tmpl.Spec.RunAsUser != nil || tmpl.Spec.RunAsNonRoot != nil {
		pod.Spec.SecurityContext = &corev1.PodSecurityContext{}
		if tmpl.Spec.RunAsUser != nil {
			pod.Spec.SecurityContext.RunAsUser = tmpl.Spec.RunAsUser
			pod.Spec.SecurityContext.RunAsGroup = tmpl.Spec.RunAsUser // same UID for group
			pod.Spec.SecurityContext.FSGroup = tmpl.Spec.RunAsUser
		}
		if tmpl.Spec.RunAsNonRoot != nil {
			pod.Spec.SecurityContext.RunAsNonRoot = tmpl.Spec.RunAsNonRoot
		}
	}

	return r.Create(ctx, pod)
}

// reconcileNetworkPolicy ensures a default-deny NetworkPolicy exists for
// sandbox pods in this pool. It allows DNS egress (UDP/TCP 53) and any
// additional egress rules from the template.
func (r *SandboxWarmPoolReconciler) reconcileNetworkPolicy(ctx context.Context, pool *volundv1.SandboxWarmPool, tmpl *volundv1.SandboxTemplate) error {
	npName := fmt.Sprintf("sandbox-%s-netpol", pool.Name)
	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"volund.io/sandbox-pool": pool.Name,
		},
	}

	// Build egress rules: DNS is always allowed.
	dnsPort53 := intstr.FromInt32(53)
	egressRules := []networkingv1.NetworkPolicyEgressRule{
		{
			// Allow DNS (UDP + TCP port 53).
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolUDP), Port: &dnsPort53},
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &dnsPort53},
			},
		},
	}

	// Add additional egress rules from the template.
	for _, rule := range tmpl.Spec.AllowedEgress {
		egressRule := networkingv1.NetworkPolicyEgressRule{}
		if rule.CIDR != "" {
			egressRule.To = []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: rule.CIDR,
					},
				},
			}
		}
		for _, port := range rule.Ports {
			p := intstr.FromInt32(int32(port))
			egressRule.Ports = append(egressRule.Ports, networkingv1.NetworkPolicyPort{
				Protocol: protocolPtr(corev1.ProtocolTCP),
				Port:     &p,
			})
		}
		egressRules = append(egressRules, egressRule)
	}

	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: pool.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "volund-operator",
				"volund.io/sandbox-pool":         pool.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(pool, volundv1.GroupVersion.WithKind("SandboxWarmPool")),
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			// Default-deny ingress (no ingress rules).
			Ingress: []networkingv1.NetworkPolicyIngressRule{},
			Egress:  egressRules,
		},
	}

	var existing networkingv1.NetworkPolicy
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if errors.IsNotFound(err) {
		log.FromContext(ctx).Info("creating sandbox NetworkPolicy", "name", npName)
		return r.Create(ctx, desired)
	} else if err != nil {
		return fmt.Errorf("get NetworkPolicy: %w", err)
	}

	// NetworkPolicy already exists; update if needed by replacing spec.
	existing.Spec = desired.Spec
	return r.Update(ctx, &existing)
}

// SetupWithManager registers the SandboxWarmPool controller with the manager.
func (r *SandboxWarmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&volundv1.SandboxWarmPool{}).
		Owns(&corev1.Pod{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Complete(r)
}

// ── SandboxClaimReconciler ──────────────────────────────────────────────────

// SandboxClaimReconciler reconciles SandboxClaim objects.
// On create, it finds an unclaimed pod from the referenced pool, labels
// it as claimed, and sets the status endpoint. On delete, it releases
// the pod by deleting it so the pool controller can replace it.
type SandboxClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=volund.ai,resources=sandboxclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=volund.ai,resources=sandboxclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch;delete

func (r *SandboxClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var claim volundv1.SandboxClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle finalizer for cleanup on deletion.
	finalizerName := "volund.ai/sandbox-claim-cleanup"

	// If the claim is being deleted, release the pod.
	if !claim.DeletionTimestamp.IsZero() {
		if containsFinalizer(claim.Finalizers, finalizerName) {
			if err := r.releasePod(ctx, &claim); err != nil {
				logger.Error(err, "failed to release sandbox pod")
				return ctrl.Result{}, err
			}
			// Remove finalizer.
			claim.Finalizers = removeFinalizer(claim.Finalizers, finalizerName)
			if err := r.Update(ctx, &claim); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is set.
	if !containsFinalizer(claim.Finalizers, finalizerName) {
		claim.Finalizers = append(claim.Finalizers, finalizerName)
		if err := r.Update(ctx, &claim); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// If already bound or failed, nothing to do.
	if claim.Status.Phase == "Bound" || claim.Status.Phase == "Failed" {
		// Check TTL expiry.
		if claim.Status.Phase == "Bound" && claim.Spec.TTLSeconds != nil && claim.Status.BoundAt != nil {
			ttl := time.Duration(*claim.Spec.TTLSeconds) * time.Second
			if time.Since(claim.Status.BoundAt.Time) > ttl {
				logger.Info("sandbox claim TTL expired, releasing", "claim", claim.Name)
				claim.Status.Phase = "Released"
				if err := r.Status().Update(ctx, &claim); err != nil && !errors.IsConflict(err) {
					return ctrl.Result{}, err
				}
				// Delete the claim to trigger cleanup.
				if err := r.Delete(ctx, &claim); client.IgnoreNotFound(err) != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			// Requeue to check TTL again.
			remaining := ttl - time.Since(claim.Status.BoundAt.Time)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
		return ctrl.Result{}, nil
	}

	// Set initial phase.
	if claim.Status.Phase == "" {
		claim.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, &claim); err != nil && !errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
	}

	// Try to bind to a warm pod from the pool.
	logger.Info("attempting to bind sandbox claim",
		"claim", claim.Name, "poolRef", claim.Spec.PoolRef)

	// Find a warm pod in the referenced pool.
	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{
			"volund.io/sandbox-pool":         claim.Spec.PoolRef,
			"volund.io/sandbox-state":        "warm",
			"app.kubernetes.io/managed-by": "volund-operator",
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list warm sandbox pods: %w", err)
	}

	// Filter to only running pods that are not being deleted.
	var availablePod *corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}
		availablePod = pod
		break
	}

	if availablePod == nil {
		// No warm pods available — set Failed status.
		logger.Info("no warm sandbox pods available", "pool", claim.Spec.PoolRef)
		claim.Status.Phase = "Failed"
		claim.Status.Error = fmt.Sprintf("no warm pods available in pool %q", claim.Spec.PoolRef)
		if err := r.Status().Update(ctx, &claim); err != nil && !errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Claim the pod: update its state label.
	availablePod.Labels["volund.io/sandbox-state"] = "claimed"
	availablePod.Labels["volund.io/sandbox-claim"] = claim.Name
	availablePod.Labels["volund.io/tenant"] = claim.Spec.TenantID
	if err := r.Update(ctx, availablePod); err != nil {
		return ctrl.Result{}, fmt.Errorf("update pod labels: %w", err)
	}

	// Build the endpoint URL. The sandbox runtime listens on port 8888.
	endpoint := fmt.Sprintf("http://%s:8888", availablePod.Status.PodIP)
	if availablePod.Status.PodIP == "" {
		// Pod doesn't have an IP yet; use the pod name as a placeholder.
		endpoint = fmt.Sprintf("http://%s.%s.svc.cluster.local:8888",
			availablePod.Name, availablePod.Namespace)
	}

	// Update claim status to Bound.
	now := metav1.Now()
	claim.Status.Phase = "Bound"
	claim.Status.PodName = availablePod.Name
	claim.Status.Endpoint = endpoint
	claim.Status.BoundAt = &now
	claim.Status.Error = ""
	if err := r.Status().Update(ctx, &claim); err != nil && !errors.IsConflict(err) {
		return ctrl.Result{}, err
	}

	logger.Info("sandbox claim bound",
		"claim", claim.Name,
		"pod", availablePod.Name,
		"endpoint", endpoint)

	// If TTL is set, requeue to check expiry.
	if claim.Spec.TTLSeconds != nil {
		ttl := time.Duration(*claim.Spec.TTLSeconds) * time.Second
		return ctrl.Result{RequeueAfter: ttl}, nil
	}

	return ctrl.Result{}, nil
}

// releasePod deletes the sandbox pod bound to this claim.
// The pool controller will recreate a fresh warm pod.
func (r *SandboxClaimReconciler) releasePod(ctx context.Context, claim *volundv1.SandboxClaim) error {
	if claim.Status.PodName == "" {
		return nil
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claim.Status.PodName,
			Namespace: claim.Namespace,
		},
	}
	if err := r.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete claimed pod %s: %w", claim.Status.PodName, err)
	}

	log.FromContext(ctx).Info("released sandbox pod",
		"claim", claim.Name, "pod", claim.Status.PodName)
	return nil
}

// SetupWithManager registers the SandboxClaim controller with the manager.
func (r *SandboxClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&volundv1.SandboxClaim{}).
		Complete(r)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func boolPtr(b bool) *bool {
	return &b
}

func protocolPtr(p corev1.Protocol) *corev1.Protocol {
	return &p
}

func containsFinalizer(finalizers []string, name string) bool {
	for _, f := range finalizers {
		if f == name {
			return true
		}
	}
	return false
}

func removeFinalizer(finalizers []string, name string) []string {
	var result []string
	for _, f := range finalizers {
		if f != name {
			result = append(result, f)
		}
	}
	return result
}
