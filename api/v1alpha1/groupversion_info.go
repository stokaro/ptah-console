// Package v1alpha1 is the console's own API group, console.ptah.run: the
// registry of the clusters a hosted console reaches. It holds what decides
// whom the console trusts and what it may reach, apart from what a person may
// change about how a connection is shown.
//
// +kubebuilder:object:generate=true
// +groupName=console.ptah.run
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group and version of the registry kinds.
	GroupVersion = schema.GroupVersion{Group: "console.ptah.run", Version: "v1alpha1"}

	// SchemeBuilder adds the registry kinds to a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the registry kinds to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&ConsoleCluster{}, &ConsoleClusterList{},
		&ConsoleClusterPreferences{}, &ConsoleClusterPreferencesList{},
		&ConsoleClusterEnrollment{}, &ConsoleClusterEnrollmentList{},
		&ConsoleClusterRevocation{}, &ConsoleClusterRevocationList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
