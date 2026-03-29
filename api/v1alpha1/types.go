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
	SchemeBuilder.Register(&AgentProfile{}, &AgentProfileList{})
	SchemeBuilder.Register(&Skill{}, &SkillList{})
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

// ── AgentProfile ─────────────────────────────────────────────────────────────

// AgentProfile defines an agent's identity — system prompt, model config,
// skills, and delegation rules. Injected into warm pool pods at claim time.
//
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.profileType`
// +kubebuilder:printcolumn:name="Visibility",type=string,JSONPath=`.spec.visibility`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.model.provider`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type AgentProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentProfileSpec   `json:"spec,omitempty"`
	Status AgentProfileStatus `json:"status,omitempty"`
}

// AgentProfileSpec defines the desired configuration for an agent profile.
type AgentProfileSpec struct {
	// DisplayName is the human-readable name.
	DisplayName string `json:"displayName"`
	// Description describes what this agent does.
	Description string `json:"description,omitempty"`
	// ProfileType is "orchestrator" or "specialist".
	// +kubebuilder:validation:Enum=orchestrator;specialist
	ProfileType string `json:"profileType"`
	// Visibility controls who can see and use this agent profile.
	// "system" profiles are visible to all users in the tenant.
	// "user" profiles are only visible to the owner.
	// +kubebuilder:validation:Enum=system;user
	// +kubebuilder:default=system
	Visibility string `json:"visibility,omitempty"`
	// OwnerID is the user ID of the profile creator. Empty for system profiles.
	// Set automatically from the user's JWT when creating user-scoped profiles.
	OwnerID string `json:"ownerId,omitempty"`
	// SystemPrompt is the system prompt injected into conversations.
	SystemPrompt string `json:"systemPrompt"`
	// Model configures the LLM.
	Model ModelConfig `json:"model"`
	// Skills lists available tool/skill names.
	Skills []string `json:"skills,omitempty"`
	// Delegation configures sub-agent spawning rules.
	Delegation *DelegationConfig `json:"delegation,omitempty"`
	// MaxToolRounds limits tool call rounds per turn.
	// +kubebuilder:default=25
	MaxToolRounds int32 `json:"maxToolRounds,omitempty"`
}

// ModelConfig holds the LLM provider and model settings.
type ModelConfig struct {
	// Provider is the LLM provider (e.g. "anthropic", "openai").
	Provider string `json:"provider"`
	// Name is the model name (e.g. "claude-sonnet-4-20250514").
	Name string `json:"name"`
	// Temperature controls randomness in generation.
	Temperature float32 `json:"temperature,omitempty"`
	// MaxTokens is the maximum number of tokens to generate.
	MaxTokens int32 `json:"maxTokens,omitempty"`
}

// DelegationConfig controls sub-agent spawning behavior.
type DelegationConfig struct {
	// CanDelegate enables this agent to spawn sub-agents.
	CanDelegate bool `json:"canDelegate"`
	// MaxConcurrent is the maximum number of concurrent sub-agent tasks.
	MaxConcurrent int32 `json:"maxConcurrent,omitempty"`
	// AllowedProfiles lists profile names this agent may delegate to.
	AllowedProfiles []string `json:"allowedProfiles,omitempty"`
}

// AgentProfileStatus reflects the observed state of the profile.
type AgentProfileStatus struct {
	// Conditions holds standard Kubernetes condition types.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type AgentProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentProfile `json:"items"`
}

// ── Skill ─────────────────────────────────────────────────────────────────────

// Skill defines a reusable capability that can be attached to AgentProfiles.
// Skills come in three tiers: prompt (injected into system prompt), mcp
// (sidecar MCP server), and cli (CLI binary wrapped in an MCP adapter).
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Author",type=string,JSONPath=`.spec.author`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Skill struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SkillSpec   `json:"spec,omitempty"`
	Status SkillStatus `json:"status,omitempty"`
}

// SkillSpec defines the desired configuration for a skill.
type SkillSpec struct {
	// Type determines how the skill is loaded at runtime.
	// +kubebuilder:validation:Enum=prompt;mcp;cli
	Type string `json:"type"`

	// Version is the semantic version of the skill.
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Description describes what this skill does.
	Description string `json:"description"`

	// Author is the skill author or organization.
	// +optional
	Author string `json:"author,omitempty"`

	// Tags are searchable labels for the skill registry.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// Parameters defines the input parameters this skill accepts.
	// Exposed to the LLM as tool parameters for MCP/CLI skills.
	// +optional
	Parameters []SkillParameter `json:"parameters,omitempty"`

	// Prompt is the markdown content injected into the system prompt.
	// Required when type is "prompt", ignored otherwise.
	// +optional
	Prompt string `json:"prompt,omitempty"`

	// Runtime configures the container for MCP server skills.
	// Required when type is "mcp", ignored for "prompt".
	// +optional
	Runtime *SkillRuntime `json:"runtime,omitempty"`

	// CLI configures the CLI binary wrapper.
	// Required when type is "cli", ignored otherwise.
	// +optional
	CLI *SkillCLI `json:"cli,omitempty"`
}

// SkillParameter defines a single input parameter for the skill.
type SkillParameter struct {
	// Name is the parameter name.
	Name string `json:"name"`
	// Type is the JSON Schema type (string, integer, number, boolean, array, object).
	Type string `json:"type"`
	// Description describes the parameter for the LLM.
	Description string `json:"description"`
	// Required indicates whether this parameter must be provided.
	Required bool `json:"required,omitempty"`
}

// SkillRuntime configures the container runtime for MCP/CLI skills.
type SkillRuntime struct {
	// Image is the container image running the MCP server.
	Image string `json:"image"`

	// Mode determines pod placement strategy.
	// +kubebuilder:validation:Enum=sidecar;shared
	// +kubebuilder:default=sidecar
	Mode string `json:"mode,omitempty"`

	// Transport is the MCP communication protocol.
	// +kubebuilder:validation:Enum=stdio;http-sse
	// +kubebuilder:default=stdio
	Transport string `json:"transport,omitempty"`

	// Resources specifies CPU/memory for the skill container.
	// +optional
	Resources *SkillResources `json:"resources,omitempty"`

	// Auth configures the credential requirements for this skill.
	// +optional
	Auth *SkillAuth `json:"auth,omitempty"`
}

// SkillResources defines compute resources for a skill container.
type SkillResources struct {
	// CPU request (e.g., "100m").
	CPU string `json:"cpu,omitempty"`
	// Memory request (e.g., "128Mi").
	Memory string `json:"memory,omitempty"`
}

// SkillAuth defines the authentication requirements for a skill.
type SkillAuth struct {
	// Type is the auth mechanism (e.g., "oauth2", "api-key").
	Type string `json:"type"`
	// Provider is the OAuth provider name (e.g., "github", "slack").
	Provider string `json:"provider,omitempty"`
	// Scopes lists the OAuth scopes required.
	Scopes []string `json:"scopes,omitempty"`
}

// SkillCLI configures a CLI binary wrapper skill.
type SkillCLI struct {
	// Binary is the CLI executable name (must be in agent image PATH).
	Binary string `json:"binary"`
	// AllowedCommands lists the permitted subcommands (e.g., ["pr view", "pr list"]).
	AllowedCommands []string `json:"allowedCommands"`
}

// SkillStatus reflects the observed state of the skill.
type SkillStatus struct {
	// Conditions holds standard Kubernetes condition types.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type SkillList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Skill `json:"items"`
}

// DeepCopyObject implements runtime.Object for Skill.
func (in *Skill) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of Skill.
func (in *Skill) DeepCopy() *Skill {
	if in == nil {
		return nil
	}
	out := new(Skill)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (in *Skill) DeepCopyInto(out *Skill) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies SkillSpec.
func (in *SkillSpec) DeepCopyInto(out *SkillSpec) {
	*out = *in
	if in.Tags != nil {
		out.Tags = make([]string, len(in.Tags))
		copy(out.Tags, in.Tags)
	}
	if in.Parameters != nil {
		out.Parameters = make([]SkillParameter, len(in.Parameters))
		copy(out.Parameters, in.Parameters)
	}
	if in.Runtime != nil {
		out.Runtime = new(SkillRuntime)
		in.Runtime.DeepCopyInto(out.Runtime)
	}
	if in.CLI != nil {
		out.CLI = new(SkillCLI)
		in.CLI.DeepCopyInto(out.CLI)
	}
}

// DeepCopyInto copies SkillRuntime.
func (in *SkillRuntime) DeepCopyInto(out *SkillRuntime) {
	*out = *in
	if in.Resources != nil {
		out.Resources = new(SkillResources)
		*out.Resources = *in.Resources
	}
	if in.Auth != nil {
		out.Auth = new(SkillAuth)
		in.Auth.DeepCopyInto(out.Auth)
	}
}

// DeepCopyInto copies SkillAuth.
func (in *SkillAuth) DeepCopyInto(out *SkillAuth) {
	*out = *in
	if in.Scopes != nil {
		out.Scopes = make([]string, len(in.Scopes))
		copy(out.Scopes, in.Scopes)
	}
}

// DeepCopyInto copies SkillCLI.
func (in *SkillCLI) DeepCopyInto(out *SkillCLI) {
	*out = *in
	if in.AllowedCommands != nil {
		out.AllowedCommands = make([]string, len(in.AllowedCommands))
		copy(out.AllowedCommands, in.AllowedCommands)
	}
}

// DeepCopyInto copies SkillStatus.
func (in *SkillStatus) DeepCopyInto(out *SkillStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		copy(out.Conditions, in.Conditions)
	}
}

// DeepCopyObject implements runtime.Object for SkillList.
func (in *SkillList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopy creates a deep copy of SkillList.
func (in *SkillList) DeepCopy() *SkillList {
	if in == nil {
		return nil
	}
	out := new(SkillList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies SkillList.
func (in *SkillList) DeepCopyInto(out *SkillList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]Skill, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyObject implements runtime.Object for AgentProfile.
func (in *AgentProfile) DeepCopyObject() runtime.Object {
	out := in.DeepCopy()
	return out
}

// DeepCopy creates a deep copy of AgentProfile.
func (in *AgentProfile) DeepCopy() *AgentProfile {
	if in == nil {
		return nil
	}
	out := new(AgentProfile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (in *AgentProfile) DeepCopyInto(out *AgentProfile) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies AgentProfileSpec.
func (in *AgentProfileSpec) DeepCopyInto(out *AgentProfileSpec) {
	*out = *in
	out.Model = in.Model
	if in.Skills != nil {
		in, out := &in.Skills, &out.Skills
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Delegation != nil {
		in, out := &in.Delegation, &out.Delegation
		*out = new(DelegationConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopyInto copies ModelConfig.
func (in *ModelConfig) DeepCopyInto(out *ModelConfig) {
	*out = *in
}

// DeepCopyInto copies DelegationConfig.
func (in *DelegationConfig) DeepCopyInto(out *DelegationConfig) {
	*out = *in
	if in.AllowedProfiles != nil {
		in, out := &in.AllowedProfiles, &out.AllowedProfiles
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopyInto copies AgentProfileStatus.
func (in *AgentProfileStatus) DeepCopyInto(out *AgentProfileStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		copy(*out, *in)
	}
}

// DeepCopyObject implements runtime.Object for AgentProfileList.
func (in *AgentProfileList) DeepCopyObject() runtime.Object {
	out := in.DeepCopy()
	return out
}

// DeepCopy creates a deep copy of AgentProfileList.
func (in *AgentProfileList) DeepCopy() *AgentProfileList {
	if in == nil {
		return nil
	}
	out := new(AgentProfileList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies AgentProfileList.
func (in *AgentProfileList) DeepCopyInto(out *AgentProfileList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]AgentProfile, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}
