// Package v1alpha1 contains the Volund operator CRD API types.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is the group and version for all Volund CRDs.
var GroupVersion = schema.GroupVersion{Group: "volund.ai", Version: "v1alpha1"}

// SchemeBuilder registers Volund types with the runtime scheme.
var (
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&AgentWarmPool{}, &AgentWarmPoolList{})
	SchemeBuilder.Register(&AgentInstance{}, &AgentInstanceList{})
}

// ── AgentWarmPool ─────────────────────────────────────────────────────────────

// AgentWarmPool maintains a pool of pre-warmed generic agent pods for a
// tenant. Agents are claimed on demand and released back to the pool.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type AgentWarmPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentWarmPoolSpec   `json:"spec,omitempty"`
	Status AgentWarmPoolStatus `json:"status,omitempty"`
}

// AgentWarmPoolSpec defines the desired state of the pool.
type AgentWarmPoolSpec struct {
	// Replicas is the target number of warm (unclaimed) pods to maintain.
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`

	// TenantID is the Volund tenant that owns this pool.
	// +kubebuilder:validation:Required
	TenantID string `json:"tenantId"`

	// ProfileID is the agent profile to inject into claimed pods.
	// +optional
	ProfileID string `json:"profileId,omitempty"`

	// Image is the volund-agent container image to run.
	// +kubebuilder:default="ghcr.io/ai-volund/volund-agent:latest"
	Image string `json:"image"`

	// LLMRouterAddr is the gRPC address of the control plane LLM service.
	// +kubebuilder:default="volund-controlplane:9091"
	LLMRouterAddr string `json:"llmRouterAddr"`

	// NATSUrl is the NATS server URL for event publishing.
	// +optional
	NATSUrl string `json:"natsUrl,omitempty"`

	// IdleTimeoutSeconds is how long a warm pod waits before going cold.
	// +kubebuilder:default=300
	IdleTimeoutSeconds int32 `json:"idleTimeoutSeconds"`
}

// AgentWarmPoolStatus reflects the observed state of the pool.
type AgentWarmPoolStatus struct {
	// ReadyReplicas is the number of warm (unclaimed) pods ready to accept work.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ActiveReplicas is the number of currently claimed (busy) pods.
	ActiveReplicas int32 `json:"activeReplicas,omitempty"`

	// Conditions holds standard Kubernetes condition types.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type AgentWarmPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentWarmPool `json:"items"`
}

// ── AgentInstance ─────────────────────────────────────────────────────────────

// AgentInstance represents a single agent pod — warm, active, or being
// recycled. The pool controller creates and manages these.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.spec.poolName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type AgentInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentInstanceSpec   `json:"spec,omitempty"`
	Status AgentInstanceStatus `json:"status,omitempty"`
}

// AgentInstanceSpec is the desired state for a single agent pod.
type AgentInstanceSpec struct {
	// PoolName is the AgentWarmPool that owns this instance.
	PoolName string `json:"poolName"`

	// TenantID identifies which tenant this agent belongs to.
	TenantID string `json:"tenantId"`

	// ProfileID is the agent profile injected at claim time.
	// +optional
	ProfileID string `json:"profileId,omitempty"`

	// Image is the container image for this agent pod.
	Image string `json:"image"`

	// LLMRouterAddr is the gRPC control-plane address.
	LLMRouterAddr string `json:"llmRouterAddr"`

	// NATSUrl is the NATS address for events.
	// +optional
	NATSUrl string `json:"natsUrl,omitempty"`
}

// AgentInstanceStatus is the observed state of a single agent pod.
type AgentInstanceStatus struct {
	// State is one of: pending, warm, active, terminating.
	// +kubebuilder:validation:Enum=pending;warm;active;terminating
	State string `json:"state,omitempty"`

	// PodName is the name of the managed Pod.
	PodName string `json:"podName,omitempty"`

	// ClaimedBy is the task or conversation ID that claimed this instance.
	// +optional
	ClaimedBy string `json:"claimedBy,omitempty"`

	// LastHeartbeat is the last time the agent pod sent a heartbeat.
	// +optional
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`

	// Conditions holds standard Kubernetes condition types.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type AgentInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentInstance `json:"items"`
}

// DeepCopyObject implements runtime.Object for AgentWarmPool.
func (in *AgentWarmPool) DeepCopyObject() runtime.Object {
	out := in.DeepCopy()
	return out
}

// DeepCopy creates a deep copy of AgentWarmPool.
func (in *AgentWarmPool) DeepCopy() *AgentWarmPool {
	if in == nil {
		return nil
	}
	out := new(AgentWarmPool)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (in *AgentWarmPool) DeepCopyInto(out *AgentWarmPool) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies AgentWarmPoolStatus.
func (in *AgentWarmPoolStatus) DeepCopyInto(out *AgentWarmPoolStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		copy(*out, *in)
	}
}

// DeepCopyObject implements runtime.Object for AgentWarmPoolList.
func (in *AgentWarmPoolList) DeepCopyObject() runtime.Object {
	out := in.DeepCopy()
	return out
}

// DeepCopy creates a deep copy of AgentWarmPoolList.
func (in *AgentWarmPoolList) DeepCopy() *AgentWarmPoolList {
	if in == nil {
		return nil
	}
	out := new(AgentWarmPoolList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies AgentWarmPoolList.
func (in *AgentWarmPoolList) DeepCopyInto(out *AgentWarmPoolList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]AgentWarmPool, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopyObject implements runtime.Object for AgentInstance.
func (in *AgentInstance) DeepCopyObject() runtime.Object {
	out := in.DeepCopy()
	return out
}

// DeepCopy creates a deep copy of AgentInstance.
func (in *AgentInstance) DeepCopy() *AgentInstance {
	if in == nil {
		return nil
	}
	out := new(AgentInstance)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies AgentInstance.
func (in *AgentInstance) DeepCopyInto(out *AgentInstance) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies AgentInstanceStatus.
func (in *AgentInstanceStatus) DeepCopyInto(out *AgentInstanceStatus) {
	*out = *in
	if in.LastHeartbeat != nil {
		in, out := &in.LastHeartbeat, &out.LastHeartbeat
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		copy(*out, *in)
	}
}

// DeepCopyObject implements runtime.Object for AgentInstanceList.
func (in *AgentInstanceList) DeepCopyObject() runtime.Object {
	out := in.DeepCopy()
	return out
}

// DeepCopy creates a deep copy of AgentInstanceList.
func (in *AgentInstanceList) DeepCopy() *AgentInstanceList {
	if in == nil {
		return nil
	}
	out := new(AgentInstanceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies AgentInstanceList.
func (in *AgentInstanceList) DeepCopyInto(out *AgentInstanceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]AgentInstance, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}
