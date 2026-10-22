package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RevocationAction is why a connection's identity is fenced.
// +kubebuilder:validation:Enum=Quarantine;Remove
type RevocationAction string

const (
	// RevocationQuarantine fences the connection's channel up to a
	// generation. It is lifted by new credentials at a higher generation.
	RevocationQuarantine RevocationAction = "Quarantine"
	// RevocationRemove fences every generation of a removed connection: the
	// tombstone that keeps a recreated entry from reviving it.
	RevocationRemove RevocationAction = "Remove"
)

// ConsoleClusterRevocation fences a connection's identity. A channel of the
// connection UID at the generation it names, or at any earlier one, is
// refused, and so is every credential the connection had then; without a
// generation, every generation is. It outlives the registry entry, so
// recreating the entry, as a GitOps reapply of the old record does, revives
// nothing. Only the console's own identity writes it; administrators read
// it.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=ccr
// +kubebuilder:printcolumn:name="Connection",type=string,JSONPath=`.spec.connectionUID`
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Generation",type=integer,JSONPath=`.spec.generation`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ConsoleClusterRevocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is what is fenced. It is immutable.
	Spec ConsoleClusterRevocationSpec `json:"spec"`
}

// ConsoleClusterRevocationSpec is one fence.
//
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="a revocation cannot be edited"
// +kubebuilder:validation:XValidation:rule="self.action == 'Remove' ? !has(self.generation) : has(self.generation)",message="a quarantine fences up to a generation, and a removal fences every generation"
type ConsoleClusterRevocationSpec struct {
	// ConnectionUID is the fenced connection.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ConnectionUID string `json:"connectionUID"`
	// Generation is the highest fenced generation; unset is every one.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Generation *int64 `json:"generation,omitempty"`
	// Action is why.
	Action RevocationAction `json:"action"`
	// ClusterName is the entry's name when it was fenced, for a person
	// reading the record.
	// +kubebuilder:validation:MaxLength=63
	ClusterName string `json:"clusterName"`
	// Reason is what the administrator gave.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	Reason string `json:"reason,omitempty"`
	// RevokedBy is the Console account that asked for it.
	// +kubebuilder:validation:MaxLength=63
	RevokedBy string `json:"revokedBy"`
}

// ConsoleClusterRevocationList is a list of ConsoleClusterRevocation.
//
// +kubebuilder:object:root=true
type ConsoleClusterRevocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConsoleClusterRevocation `json:"items"`
}
