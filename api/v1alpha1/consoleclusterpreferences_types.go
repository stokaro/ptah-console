package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConsoleClusterPreferences is what a person may change about how a
// connection is shown, apart from the connection's trust. Editing it grants
// nothing: it names the connection by UID, and a ConsoleCluster recreated
// under the same name is another connection these preferences do not follow.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=cclp
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Display name",type=string,JSONPath=`.spec.displayName`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
type ConsoleClusterPreferences struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the connection reference and the preferences.
	Spec ConsoleClusterPreferencesSpec `json:"spec"`
}

// ConsoleClusterPreferencesSpec names a connection and how it is shown.
type ConsoleClusterPreferencesSpec struct {
	// ClusterRef is the connection these preferences belong to, by name and
	// connection UID. It is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterRef is immutable"
	ClusterRef ConnectionReference `json:"clusterRef"`

	// DisplayName is the name people see for the connection.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[^\x00-\x1f\x7f]*$`
	DisplayName string `json:"displayName,omitempty"`

	// Labels group and filter connections in the console. They carry no
	// authority: no grant, route or trust decision reads them.
	// +optional
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=key
	Labels []PreferenceLabel `json:"labels,omitempty"`

	// Enabled is whether the console offers the connection; unset is true. A
	// disabled connection keeps its configuration and dispatches nothing.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// PreferenceLabel is one label of a connection's preferences.
type PreferenceLabel struct {
	// Key is a DNS label.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Key string `json:"key"`
	// Value is at most 63 characters with no control character.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[^\x00-\x1f\x7f]*$`
	Value string `json:"value"`
}

// ConnectionReference names one connection by name and connection UID.
type ConnectionReference struct {
	// Name of the ConsoleCluster, in the same namespace.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name"`
	// ConnectionUID is the connection UID the ConsoleCluster had.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ConnectionUID string `json:"connectionUID"`
}

// ConsoleClusterPreferencesList is a list of ConsoleClusterPreferences.
//
// +kubebuilder:object:root=true
type ConsoleClusterPreferencesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConsoleClusterPreferences `json:"items"`
}
