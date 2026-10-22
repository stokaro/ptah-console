package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConnectionProfile is how the console reaches a cluster.
// +kubebuilder:validation:Enum=CoLocated;Agent
type ConnectionProfile string

const (
	// ProfileCoLocated is the cluster the console runs in, reached through
	// the console's own impersonating client.
	ProfileCoLocated ConnectionProfile = "CoLocated"
	// ProfileAgent is another cluster, reached through the agent its
	// administrator installed there, which dials the console.
	ProfileAgent ConnectionProfile = "Agent"
)

// ConfigurationOwner says who edits a registry entry.
// +kubebuilder:validation:Enum=Console;GitOps
type ConfigurationOwner string

const (
	// OwnerConsole entries are created and removed by installation
	// administrators through the console.
	OwnerConsole ConfigurationOwner = "Console"
	// OwnerGitOps entries are applied from a repository. The console reads
	// and exports them and edits nothing.
	OwnerGitOps ConfigurationOwner = "GitOps"
)

// ConsoleCluster is one connection of a hosted console to a cluster: what the
// console trusts about it and what it may reach there. Its spec cannot be
// edited. A change of trust, profile or scope is a new connection with a new
// connection UID, enrolled again, and the old one is revoked; nothing is
// retargeted in place.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ccl
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profile`
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.configurationOwner`
// +kubebuilder:printcolumn:name="Connected",type=string,JSONPath=`.status.conditions[?(@.type=="Connected")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ConsoleCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the connection's trust and reach. It is immutable.
	Spec ConsoleClusterSpec `json:"spec"`
	// Status is what the console's own identity last observed about the
	// connection. Nothing else writes it.
	// +optional
	Status ConsoleClusterStatus `json:"status,omitempty"`
}

// ConsoleClusterSpec is everything that decides whom the console trusts and
// what it may reach through one connection.
//
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="a connection's trust and reach are immutable; enroll a new connection instead"
// +kubebuilder:validation:XValidation:rule="(self.profile == 'Agent') == has(self.agent)",message="an Agent connection names its agent trust, and a CoLocated one names none"
type ConsoleClusterSpec struct {
	// ConnectionUID is the connection's identity. Every route, cached view,
	// confirmation and request names it, so two clusters with the same name
	// cannot be confused, and a new connection to the same cluster is told
	// apart from the one it replaces.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ConnectionUID string `json:"connectionUID"`

	// Profile is how the console reaches the cluster.
	Profile ConnectionProfile `json:"profile"`

	// InstallationID is the installation whose accounts act through this
	// connection, as ptah-console:<installationID>:<account id>. For the
	// cluster the console runs in it is the console's own installation ID, so
	// no account loses a grant when the registry is installed.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	InstallationID string `json:"installationID"`

	// Namespaces is the ceiling: the namespaces the console reads and acts in
	// through this connection. An account's own RBAC may narrow it further,
	// never widen it.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=set
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespaces []string `json:"namespaces"`

	// ConfigurationOwner says who edits this entry.
	ConfigurationOwner ConfigurationOwner `json:"configurationOwner"`

	// Agent is the trust of an Agent connection, as the target administrator
	// approved it at enrollment. A CoLocated connection has none.
	// +optional
	Agent *AgentTrust `json:"agent,omitempty"`
}

// AgentTrust is what an agent connection was enrolled with: the target
// cluster's API server as the target administrator's helper reported it, and
// the console identity the agent accepts.
type AgentTrust struct {
	// TargetServer is the target cluster's API server address as enrolled. The
	// console never dials it; it identifies the cluster to a person.
	// +kubebuilder:validation:MinLength=9
	// +kubebuilder:validation:MaxLength=512
	// +kubebuilder:validation:Pattern=`^https://[^[:space:]/?#@]+(/[^[:space:]?#]*)?$`
	TargetServer string `json:"targetServer"`
	// TargetCASHA256 is the SHA-256 of the CA bundle the target's API server
	// presented at enrollment.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	TargetCASHA256 string `json:"targetCASHA256"`
	// ConsoleServerName is the TLS name the agent requires of the console.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	ConsoleServerName string `json:"consoleServerName"`
	// ConsoleCASHA256 is the SHA-256 of the console's own CA, the only one
	// the agent accepts the console under.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ConsoleCASHA256 string `json:"consoleCASHA256"`
}

// ConditionConnected says whether the console can reach the cluster through
// the connection, as last observed. It says nothing about any account's
// rights there.
const ConditionConnected = "Connected"

// ConsoleClusterStatus is the bounded, machine-written account of a
// connection.
type ConsoleClusterStatus struct {
	// ObservedGeneration is the spec generation this status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions are the connection's verdicts, Connected among them.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// LastObservedAt is when the console last heard from the cluster through
	// this connection.
	// +optional
	LastObservedAt *metav1.Time `json:"lastObservedAt,omitempty"`
}

// ConsoleClusterList is a list of ConsoleCluster.
//
// +kubebuilder:object:root=true
type ConsoleClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConsoleCluster `json:"items"`
}
