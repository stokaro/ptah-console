package registry_test

import (
	"testing"

	qt "github.com/frankban/quicktest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/registry"
)

const connection = "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f"

func entry() *consolev1alpha1.ConsoleCluster {
	return &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ptah-console", Name: "this-cluster-3f2c4b1e"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: connection, Profile: consolev1alpha1.ProfileCoLocated, InstallationID: "prod-eu-1",
			Namespaces: []string{"billing", "application"}, ConfigurationOwner: consolev1alpha1.OwnerGitOps,
		},
	}
}

func want() registry.ThisCluster {
	return registry.ThisCluster{
		Namespace: "ptah-console", Name: "this-cluster-3f2c4b1e", ConnectionUID: connection,
		InstallationID: "prod-eu-1", Namespaces: []string{"application", "billing"},
	}
}

func TestCheckThisClusterHoldsTheEntryToTheConfiguration(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	scheme, err := kube.Scheme()
	c.Assert(err, qt.IsNil)
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry()).Build()
	c.Assert(registry.CheckThisCluster(t.Context(), reader, want()), qt.IsNil, qt.Commentf("the order of the namespaces is not part of the ceiling"))

	for name, mutate := range map[string]func(*registry.ThisCluster){
		"another connection":   func(w *registry.ThisCluster) { w.ConnectionUID = "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d" },
		"another installation": func(w *registry.ThisCluster) { w.InstallationID = "prod-us-1" },
		"a wider ceiling":      func(w *registry.ThisCluster) { w.Namespaces = append(w.Namespaces, "payments") },
		"a narrower ceiling":   func(w *registry.ThisCluster) { w.Namespaces = []string{"application"} },
	} {
		configured := want()
		mutate(&configured)
		err := registry.CheckThisCluster(t.Context(), reader, configured)
		c.Assert(err, qt.ErrorIs, registry.ErrThisClusterDisagrees, qt.Commentf("%s", name))
	}

	agent := entry()
	agent.Spec.Profile = consolev1alpha1.ProfileAgent
	reader = fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	c.Assert(registry.CheckThisCluster(t.Context(), reader, want()), qt.ErrorMatches, `.*its profile is Agent, not CoLocated.*`)

	missing := want()
	missing.Name = "elsewhere"
	c.Assert(registry.CheckThisCluster(t.Context(), reader, missing), qt.ErrorMatches, `read the registry's entry for this cluster ptah-console/elsewhere: .*not found.*`)
}
