package api

import (
	"fmt"
	"slices"

	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// operatorResources are the operator's resources the console reads or
// writes. PtahResultRecord and PtahRealm are not among them: the console
// never reads them.
var operatorResources = []string{
	"ptahschemas", "ptahschemaplans", "ptahschemaplanchunks", "ptahschemaapprovals",
	"ptahmigrations", "ptahmigrationplans", "ptahmigrationapprovals", "ptahmigrationrunacknowledgments",
}

// operatorAPI asks discovery, as the session's identity, whether the cluster
// serves the operator's group version and the resources the console needs.
func operatorAPI(config *rest.Config) *consolev1.OperatorApi {
	if config == nil {
		return &consolev1.OperatorApi{Problem: "The console has no configuration to ask the API server with."}
	}
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return &consolev1.OperatorApi{Problem: "The console could not ask the API server what it serves."}
	}
	return answerDiscovery(client.ServerResourcesForGroupVersion(operatorv1alpha1.GroupVersion.String()))
}

// kubernetesVersion is the cluster's version as discovery tells the
// session's identity, or empty when the API server does not say.
func kubernetesVersion(config *rest.Config) string {
	if config == nil {
		return ""
	}
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return ""
	}
	version, err := client.ServerVersion()
	if err != nil {
		return ""
	}
	return version.GitVersion
}

// answerDiscovery says what one discovery answer means for the console.
func answerDiscovery(list *metav1.APIResourceList, err error) *consolev1.OperatorApi {
	groupVersion := operatorv1alpha1.GroupVersion.String()
	switch {
	case apierrors.IsNotFound(err):
		return &consolev1.OperatorApi{Problem: fmt.Sprintf(
			"This cluster does not serve %s: Ptah Operator is not installed here, or its CRDs are not.", groupVersion)}
	case err != nil:
		return &consolev1.OperatorApi{Problem: fmt.Sprintf("The API server did not say whether it serves %s.", groupVersion)}
	}
	answer := &consolev1.OperatorApi{Served: true}
	for _, resource := range operatorResources {
		if !slices.ContainsFunc(list.APIResources, func(served metav1.APIResource) bool { return served.Name == resource }) {
			answer.Missing = append(answer.Missing, resource)
		}
	}
	if len(answer.Missing) > 0 {
		answer.Problem = fmt.Sprintf("This cluster serves %s without every resource the console needs: the operator's CRDs are older or newer than this console expects.", groupVersion)
	}
	return answer
}
