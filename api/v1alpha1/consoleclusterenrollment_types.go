package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnrollmentRole is a role an account gets in every namespace of the ceiling
// on the enrolled cluster.
// +kubebuilder:validation:Enum=viewer;plan-reader;desired-state-author;diagnostic-reader;approver
type EnrollmentRole string

// ConsoleClusterEnrollment is a connection being enrolled: what an
// installation administrator asked for, held until the target administrator's
// helper completes it and an administrator activates it, or until it expires.
// It is not a connection: nothing is dispatched through it.
//
// An administrator creates it through the console, as themselves. The console
// alone writes its status: the verifier of the single-use bootstrap secret,
// when the secret expires, the enrollment's fingerprint, and what the helper
// reported when it completed.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cce
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.status.expiresAt`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completion.completedAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ConsoleClusterEnrollment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is what the administrator asked for. It is immutable.
	Spec ConsoleClusterEnrollmentSpec `json:"spec"`
	// Status is the console's record of the enrollment. Nothing else writes
	// it.
	// +optional
	Status ConsoleClusterEnrollmentStatus `json:"status,omitempty"`
}

// ConsoleClusterEnrollmentSpec is the connection an enrollment proposes: the
// entry it becomes, what the agent will be granted on the target, and the
// console identity the agent will trust.
//
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="an enrollment cannot be edited; start a new one"
type ConsoleClusterEnrollmentSpec struct {
	// ClusterName is the name the connection's ConsoleCluster will have.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ClusterName string `json:"clusterName"`
	// ConnectionUID is the new connection's identity.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ConnectionUID string `json:"connectionUID"`
	// InstallationID is the console's.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	InstallationID string `json:"installationID"`
	// Namespaces is the ceiling on the target.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=set
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespaces []string `json:"namespaces"`
	// Accounts are the Console accounts the agent will act for, with the
	// roles each gets in every namespace of the ceiling.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=256
	// +listType=map
	// +listMapKey=id
	Accounts []EnrollmentAccount `json:"accounts"`
	// ApproverClusterRole is the operator chart's approver ClusterRole on the
	// target, which the approver role binds.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!(self in ['cluster-admin', 'admin', 'edit']) && !self.startsWith('system:')",message="the approver role is the operator's approver ClusterRole, never an administrator role"
	ApproverClusterRole string `json:"approverClusterRole"`
	// ConsoleServerName is the TLS name the agent will require of the
	// console.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	ConsoleServerName string `json:"consoleServerName"`
	// ConsoleCASHA256 is the SHA-256 of the console's own CA, the only one the
	// agent will accept the console under.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ConsoleCASHA256 string `json:"consoleCASHA256"`
}

// EnrollmentAccount is one account the agent will act for.
type EnrollmentAccount struct {
	// ID is the Console account.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ID string `json:"id"`
	// Roles are what the account gets in every namespace of the ceiling.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=5
	// +listType=set
	Roles []EnrollmentRole `json:"roles"`
}

// Conditions of an enrollment.
const (
	// ConditionAgentConnected says whether the agent installed for the
	// enrollment has opened its channel. Nothing is dispatched through it
	// until the connection is activated.
	ConditionAgentConnected = "AgentConnected"
)

// ConsoleClusterEnrollmentStatus is the console's record of an enrollment.
//
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.completion) || (has(self.completion) && self.completion == oldSelf.completion)",message="a completed enrollment stays completed as it was"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.bootstrapVerifier) || (has(self.bootstrapVerifier) && self.bootstrapVerifier == oldSelf.bootstrapVerifier)",message="an enrollment's bootstrap secret is issued once"
type ConsoleClusterEnrollmentStatus struct {
	// BootstrapVerifier is the lowercase hex SHA-256 of the single-use
	// bootstrap secret. The console never keeps the secret.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	BootstrapVerifier string `json:"bootstrapVerifier,omitempty"`
	// ExpiresAt is when the bootstrap secret stops being accepted.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`
	// Fingerprint is the SHA-256 of the enrollment's identity, which the
	// console and the target administrator's helper both show.
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	Fingerprint string `json:"fingerprint,omitempty"`
	// Completion is what the helper reported, once.
	// +optional
	Completion *EnrollmentCompletion `json:"completion,omitempty"`
	// Conditions are the enrollment's verdicts.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EnrollmentCompletion is what the target administrator's helper reported:
// the target's API server as their kubeconfig names it, and the verifier of
// the channel credential it made there. The credential never leaves the
// target.
type EnrollmentCompletion struct {
	// TargetServer is the target API server, as the helper's context names
	// it.
	// +kubebuilder:validation:MinLength=9
	// +kubebuilder:validation:MaxLength=512
	// +kubebuilder:validation:Pattern=`^https://[^[:space:]/?#@]+(/[^[:space:]?#]*)?$`
	TargetServer string `json:"targetServer"`
	// TargetCASHA256 is the SHA-256 of the CA bundle the helper's context
	// trusts for it.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	TargetCASHA256 string `json:"targetCASHA256"`
	// CredentialVerifier is the lowercase hex SHA-256 of the channel
	// credential.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	CredentialVerifier string `json:"credentialVerifier"`
	// CompletedAt is when the helper completed the enrollment.
	CompletedAt metav1.Time `json:"completedAt"`
}

// ConsoleClusterEnrollmentList is a list of ConsoleClusterEnrollment.
//
// +kubebuilder:object:root=true
type ConsoleClusterEnrollmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConsoleClusterEnrollment `json:"items"`
}
