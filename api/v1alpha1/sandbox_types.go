// Package v1alpha1 contains the Volund operator CRD API types.
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	SchemeBuilder.Register(&SandboxTemplate{}, &SandboxTemplateList{})
	SchemeBuilder.Register(&SandboxWarmPool{}, &SandboxWarmPoolList{})
	SchemeBuilder.Register(&SandboxClaim{}, &SandboxClaimList{})
}

// ── SandboxTemplate ─────────────────────────────────────────────────────────

// SandboxTemplate defines a reusable sandbox pod spec for tool execution.
// One per tenant namespace, created by the operator when a tenant is provisioned.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="RuntimeClass",type=string,JSONPath=`.spec.runtimeClassName`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SandboxTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxTemplateSpec   `json:"spec,omitempty"`
	Status SandboxTemplateStatus `json:"status,omitempty"`
}

// SandboxTemplateSpec defines the desired configuration for a sandbox template.
type SandboxTemplateSpec struct {
	// RuntimeClassName for kernel isolation (e.g., "gvisor").
	// +kubebuilder:validation:Required
	RuntimeClassName string `json:"runtimeClassName"`

	// Image for the sandbox runtime container.
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// Resources for the sandbox container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NetworkPolicyMode controls network policy management.
	// "managed" means the operator creates default-deny + DNS egress.
	// "custom" means the user manages their own NetworkPolicy.
	// +kubebuilder:validation:Enum=managed;custom
	// +kubebuilder:default=managed
	// +optional
	NetworkPolicyMode string `json:"networkPolicyMode,omitempty"`

	// AllowedEgress defines additional egress rules beyond DNS (only when networkPolicyMode=managed).
	// +optional
	AllowedEgress []NetworkEgressRule `json:"allowedEgress,omitempty"`

	// RunAsUser sets the UID for the sandbox container.
	// +optional
	RunAsUser *int64 `json:"runAsUser,omitempty"`

	// RunAsNonRoot requires that the sandbox container runs as a non-root user.
	// +optional
	RunAsNonRoot *bool `json:"runAsNonRoot,omitempty"`
}

// NetworkEgressRule defines an additional egress rule for sandbox pods.
type NetworkEgressRule struct {
	// CIDR is the destination IP block (e.g., "10.0.0.0/8").
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// Ports are the allowed destination ports.
	// +optional
	Ports []int `json:"ports,omitempty"`
}

// SandboxTemplateStatus reflects the observed state of the sandbox template.
type SandboxTemplateStatus struct {
	// Ready indicates whether the template has been validated and is usable.
	Ready bool `json:"ready"`

	// Error contains a human-readable error message if the template is invalid.
	// +optional
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
type SandboxTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxTemplate `json:"items"`
}

// ── SandboxWarmPool ─────────────────────────────────────────────────────────

// SandboxWarmPool maintains pre-warmed sandbox pods for fast allocation.
// Sandbox pods are claimed by SandboxClaim resources when agents need
// to execute code in an isolated environment.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Available",type=integer,JSONPath=`.status.available`
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=`.status.active`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SandboxWarmPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxWarmPoolSpec   `json:"spec,omitempty"`
	Status SandboxWarmPoolStatus `json:"status,omitempty"`
}

// SandboxWarmPoolSpec defines the desired state of a sandbox warm pool.
type SandboxWarmPoolSpec struct {
	// Replicas is the target number of warm (unclaimed) sandbox pods.
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// TemplateRef references the SandboxTemplate to use for creating sandbox pods.
	// +kubebuilder:validation:Required
	TemplateRef string `json:"templateRef"`

	// MinReplicas for HPA scaling (optional).
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas for HPA scaling (optional).
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// ShutdownTimeoutSeconds is the maximum lifetime per sandbox pod.
	// +optional
	ShutdownTimeoutSeconds *int32 `json:"shutdownTimeoutSeconds,omitempty"`
}

// SandboxWarmPoolStatus reflects the observed state of the sandbox warm pool.
type SandboxWarmPoolStatus struct {
	// Available is the count of warm (unclaimed) pods.
	Available int32 `json:"available"`

	// Active is the count of claimed (in-use) pods.
	Active int32 `json:"active"`

	// Total is Available + Active.
	Total int32 `json:"total"`
}

// +kubebuilder:object:root=true
type SandboxWarmPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxWarmPool `json:"items"`
}

// ── SandboxClaim ────────────────────────────────────────────────────────────

// SandboxClaim is a request to claim a sandbox pod from a warm pool.
// Created by the agent when it needs to execute code, deleted when the
// conversation ends. The claim controller binds the claim to an available
// warm pod and sets the endpoint for the agent to use.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.spec.poolRef`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SandboxClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxClaimSpec   `json:"spec,omitempty"`
	Status SandboxClaimStatus `json:"status,omitempty"`
}

// SandboxClaimSpec defines the desired state of a sandbox claim.
type SandboxClaimSpec struct {
	// PoolRef references the SandboxWarmPool to claim from.
	// +kubebuilder:validation:Required
	PoolRef string `json:"poolRef"`

	// ConversationID ties this claim to a conversation lifecycle.
	// +kubebuilder:validation:Required
	ConversationID string `json:"conversationId"`

	// TenantID for ownership tracking.
	// +kubebuilder:validation:Required
	TenantID string `json:"tenantId"`

	// TTLSeconds is the auto-delete duration (safety net).
	// +optional
	TTLSeconds *int32 `json:"ttlSeconds,omitempty"`
}

// SandboxClaimStatus reflects the observed state of the sandbox claim.
type SandboxClaimStatus struct {
	// Phase is one of: Pending, Bound, Released, Failed.
	// +kubebuilder:validation:Enum=Pending;Bound;Released;Failed
	Phase string `json:"phase"`

	// PodName is the name of the claimed sandbox pod.
	// +optional
	PodName string `json:"podName,omitempty"`

	// Endpoint is the HTTP address of the sandbox runtime API.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// BoundAt is when the claim was fulfilled.
	// +optional
	BoundAt *metav1.Time `json:"boundAt,omitempty"`

	// Error message if phase is Failed.
	// +optional
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
type SandboxClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxClaim `json:"items"`
}

// ── DeepCopy methods ────────────────────────────────────────────────────────

// -- SandboxTemplate --

// DeepCopyObject implements runtime.Object for SandboxTemplate.
func (in *SandboxTemplate) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SandboxTemplate.
func (in *SandboxTemplate) DeepCopy() *SandboxTemplate {
	if in == nil {
		return nil
	}
	out := new(SandboxTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (in *SandboxTemplate) DeepCopyInto(out *SandboxTemplate) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopyInto copies SandboxTemplateSpec.
func (in *SandboxTemplateSpec) DeepCopyInto(out *SandboxTemplateSpec) {
	*out = *in
	in.Resources.DeepCopyInto(&out.Resources)
	if in.AllowedEgress != nil {
		out.AllowedEgress = make([]NetworkEgressRule, len(in.AllowedEgress))
		for i := range in.AllowedEgress {
			in.AllowedEgress[i].DeepCopyInto(&out.AllowedEgress[i])
		}
	}
	if in.RunAsUser != nil {
		in, out := &in.RunAsUser, &out.RunAsUser
		*out = new(int64)
		**out = **in
	}
	if in.RunAsNonRoot != nil {
		in, out := &in.RunAsNonRoot, &out.RunAsNonRoot
		*out = new(bool)
		**out = **in
	}
}

// DeepCopyInto copies NetworkEgressRule.
func (in *NetworkEgressRule) DeepCopyInto(out *NetworkEgressRule) {
	*out = *in
	if in.Ports != nil {
		out.Ports = make([]int, len(in.Ports))
		copy(out.Ports, in.Ports)
	}
}

// DeepCopyObject implements runtime.Object for SandboxTemplateList.
func (in *SandboxTemplateList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SandboxTemplateList.
func (in *SandboxTemplateList) DeepCopy() *SandboxTemplateList {
	if in == nil {
		return nil
	}
	out := new(SandboxTemplateList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies SandboxTemplateList.
func (in *SandboxTemplateList) DeepCopyInto(out *SandboxTemplateList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]SandboxTemplate, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// -- SandboxWarmPool --

// DeepCopyObject implements runtime.Object for SandboxWarmPool.
func (in *SandboxWarmPool) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SandboxWarmPool.
func (in *SandboxWarmPool) DeepCopy() *SandboxWarmPool {
	if in == nil {
		return nil
	}
	out := new(SandboxWarmPool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (in *SandboxWarmPool) DeepCopyInto(out *SandboxWarmPool) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopyInto copies SandboxWarmPoolSpec.
func (in *SandboxWarmPoolSpec) DeepCopyInto(out *SandboxWarmPoolSpec) {
	*out = *in
	if in.MinReplicas != nil {
		in, out := &in.MinReplicas, &out.MinReplicas
		*out = new(int32)
		**out = **in
	}
	if in.MaxReplicas != nil {
		in, out := &in.MaxReplicas, &out.MaxReplicas
		*out = new(int32)
		**out = **in
	}
	if in.ShutdownTimeoutSeconds != nil {
		in, out := &in.ShutdownTimeoutSeconds, &out.ShutdownTimeoutSeconds
		*out = new(int32)
		**out = **in
	}
}

// DeepCopyObject implements runtime.Object for SandboxWarmPoolList.
func (in *SandboxWarmPoolList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SandboxWarmPoolList.
func (in *SandboxWarmPoolList) DeepCopy() *SandboxWarmPoolList {
	if in == nil {
		return nil
	}
	out := new(SandboxWarmPoolList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies SandboxWarmPoolList.
func (in *SandboxWarmPoolList) DeepCopyInto(out *SandboxWarmPoolList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]SandboxWarmPool, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// -- SandboxClaim --

// DeepCopyObject implements runtime.Object for SandboxClaim.
func (in *SandboxClaim) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SandboxClaim.
func (in *SandboxClaim) DeepCopy() *SandboxClaim {
	if in == nil {
		return nil
	}
	out := new(SandboxClaim)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (in *SandboxClaim) DeepCopyInto(out *SandboxClaim) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies SandboxClaimSpec.
func (in *SandboxClaimSpec) DeepCopyInto(out *SandboxClaimSpec) {
	*out = *in
	if in.TTLSeconds != nil {
		in, out := &in.TTLSeconds, &out.TTLSeconds
		*out = new(int32)
		**out = **in
	}
}

// DeepCopyInto copies SandboxClaimStatus.
func (in *SandboxClaimStatus) DeepCopyInto(out *SandboxClaimStatus) {
	*out = *in
	if in.BoundAt != nil {
		in, out := &in.BoundAt, &out.BoundAt
		*out = (*in).DeepCopy()
	}
}

// DeepCopyObject implements runtime.Object for SandboxClaimList.
func (in *SandboxClaimList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SandboxClaimList.
func (in *SandboxClaimList) DeepCopy() *SandboxClaimList {
	if in == nil {
		return nil
	}
	out := new(SandboxClaimList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies SandboxClaimList.
func (in *SandboxClaimList) DeepCopyInto(out *SandboxClaimList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]SandboxClaim, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
