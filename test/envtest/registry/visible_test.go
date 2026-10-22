package registry_test

import (
	"testing"

	qt "github.com/frankban/quicktest"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/registry"
)

// clientAs is a client the API server authenticates as name.
func clientAs(t *testing.T, name string) client.Client {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(name)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	return built
}

// grant gives subject the rule in namespace through a Role and its binding.
func grant(t *testing.T, namespace, name, subject string, rule rbacv1.PolicyRule) {
	t.Helper()
	c := qt.New(t)
	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Rules: []rbacv1.PolicyRule{rule}}
	c.Assert(plane.Admin.Create(t.Context(), role), qt.IsNil)
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: subject}},
	}
	c.Assert(plane.Admin.Create(t.Context(), binding), qt.IsNil)
}

func names(entries []consolev1alpha1.ConsoleCluster) []string {
	out := make([]string, len(entries))
	for index, entry := range entries {
		out[index] = entry.Name
	}
	return out
}

// A person sees the entries they may get and nothing else: not the name of
// another, and not that another exists. What the identity that listed the
// names may get plays no part, so one that may only list sees no entry.
func TestAPersonSeesOnlyTheConnectionsTheyMayGet(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := newNamespace(t)
	c.Assert(plane.Admin.Create(t.Context(), coLocated(namespace)), qt.IsNil)
	c.Assert(plane.Admin.Create(t.Context(), agent(namespace)), qt.IsNil)

	grant(t, namespace, "housekeeping-list", "housekeeping", rbacv1.PolicyRule{
		APIGroups: []string{consolev1alpha1.GroupVersion.Group}, Resources: []string{"consoleclusters"}, Verbs: []string{"list"},
	})
	grant(t, namespace, "alice-this-cluster", "alice", rbacv1.PolicyRule{
		APIGroups: []string{consolev1alpha1.GroupVersion.Group}, Resources: []string{"consoleclusters"},
		ResourceNames: []string{"this-cluster"}, Verbs: []string{"get"},
	})
	housekeeping := clientAs(t, "housekeeping")

	alice, err := registry.Visible(t.Context(), housekeeping, clientAs(t, "alice"), namespace)
	c.Assert(err, qt.IsNil)
	c.Assert(names(alice), qt.DeepEquals, []string{"this-cluster"})
	c.Assert(alice[0].Spec.ConnectionUID, qt.Equals, thisConnection)

	nobody, err := registry.Visible(t.Context(), housekeeping, clientAs(t, "mallory"), namespace)
	c.Assert(err, qt.IsNil)
	c.Assert(nobody, qt.HasLen, 0)

	// The console's own identity lists, and the list is all it is for.
	itself, err := registry.Visible(t.Context(), housekeeping, housekeeping, namespace)
	c.Assert(err, qt.IsNil)
	c.Assert(itself, qt.HasLen, 0)

	// Without the list grant there is nothing to read the names with, and
	// the failure is reported rather than shown as an empty registry.
	_, err = registry.Visible(t.Context(), clientAs(t, "alice"), clientAs(t, "alice"), namespace)
	c.Assert(err, qt.ErrorMatches, `list the registry: .*forbidden.*`)
}
