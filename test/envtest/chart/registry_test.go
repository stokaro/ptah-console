package chart_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/registry"
)

// A second release, with the registry on, beside the first: its own name,
// namespace and installation, so none of its grants reach the first's
// accounts or ServiceAccount.
const (
	registryValues = "../../chart/testdata/registry.values.yaml"
	hubNS          = "ptah-console-hub"
	hubSA          = "system:serviceaccount:" + hubNS + ":hub-ptah-console"
	hubPrefix      = "ptah-console:prod-hub:"
	sha            = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

// installedRegistry is the registry release on the API server, with the
// registry CRDs as the chart carries them, and the configuration its
// ConfigMap gives the console.
var installedRegistry = sync.OnceValues(func() (*accounts.Config, error) {
	// The first release creates the namespaces of the ceiling.
	if _, err := installed(); err != nil {
		return nil, err
	}
	ctx := context.Background()
	rendered, err := helm("template", "hub", consoleChart, "--namespace", hubNS,
		"--values", registryValues, "--set", "installationID=prod-hub")
	if err != nil {
		return nil, err
	}
	objects, err := decode(rendered)
	if err != nil {
		return nil, err
	}
	if err := plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: hubNS}}); err != nil {
		return nil, err
	}
	var config *accounts.Config
	for _, object := range objects {
		if err := plane.Admin.Create(ctx, object); err != nil {
			return nil, fmt.Errorf("create %s %s/%s: %w", object.GetKind(), object.GetNamespace(), object.GetName(), err)
		}
		if object.GetKind() == "ConfigMap" {
			text, _, _ := unstructured.NestedString(object.Object, "data", "configuration.yaml")
			if config, err = accounts.ParseConfig([]byte(text)); err != nil {
				return nil, err
			}
		}
	}
	if config == nil || config.Registry == nil {
		return nil, fmt.Errorf("the registry render gives the console no registry: %+v", config)
	}
	return config, nil
})

func installRegistry(t *testing.T) *accounts.Config {
	t.Helper()
	config, err := installedRegistry()
	qt.New(t).Assert(err, qt.IsNil)
	return config
}

func entryNames(entries []consolev1alpha1.ConsoleCluster) []string {
	out := make([]string, len(entries))
	for index, entry := range entries {
		out[index] = entry.Name
	}
	slices.Sort(out)
	return out
}

// The console starts against the entry its chart wrote, through the grant its
// chart gave it, and a person sees through that grant the entries they may get
// and no other: a connection administrator adds one, and an account without
// the role learns nothing of it.
func TestTheConsoleReadsTheRegistryThroughItsChartsGrants(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	config := installRegistry(t)
	housekeeping := as(t, hubSA)

	c.Assert(registry.CheckThisCluster(t.Context(), housekeeping, registry.ThisCluster{
		Namespace: config.Registry.Namespace, Name: config.Registry.ThisCluster,
		ConnectionUID: config.Registry.ConnectionUID, InstallationID: config.InstallationID,
		Namespaces: config.Namespaces,
	}), qt.IsNil)

	added := &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: hubNS, Name: "prod-ap-2"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID:      "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d",
			Profile:            consolev1alpha1.ProfileAgent,
			InstallationID:     "prod-hub",
			Namespaces:         []string{"application"},
			ConfigurationOwner: consolev1alpha1.OwnerConsole,
			Agent: &consolev1alpha1.AgentTrust{
				TargetServer: "https://api.prod-ap-2.example.com:6443", TargetCASHA256: sha,
				ConsoleServerName: "agents.console.example.com", ConsoleCASHA256: sha,
			},
		},
	}
	refused := added.DeepCopy()
	c.Assert(apierrors.IsForbidden(as(t, hubPrefix+"sam").Create(t.Context(), refused)), qt.IsTrue)
	c.Assert(as(t, hubPrefix+"alex").Create(t.Context(), added), qt.IsNil)

	sam, err := registry.Visible(t.Context(), housekeeping, as(t, hubPrefix+"sam"), hubNS)
	c.Assert(err, qt.IsNil)
	c.Assert(entryNames(sam), qt.DeepEquals, []string{config.Registry.ThisCluster})

	alex, err := registry.Visible(t.Context(), housekeeping, as(t, hubPrefix+"alex"), hubNS)
	c.Assert(err, qt.IsNil)
	c.Assert(entryNames(alex), qt.DeepEquals, []string{"prod-ap-2", config.Registry.ThisCluster})
}

// What each identity of the registry release may do with the registry, as
// the API server's access review answers.
func TestEachRegistryIdentityMayDoOnlyItsPart(t *testing.T) {
	t.Parallel()
	config := installRegistry(t)
	group := consolev1alpha1.GroupVersion.Group
	entry := func(verb, name string) authorizationv1.ResourceAttributes {
		return authorizationv1.ResourceAttributes{Group: group, Resource: "consoleclusters", Namespace: hubNS, Verb: verb, Name: name}
	}
	enrollment := func(verb, name string) authorizationv1.ResourceAttributes {
		return authorizationv1.ResourceAttributes{Group: group, Resource: "consoleclusterenrollments", Namespace: hubNS, Verb: verb, Name: name}
	}
	revocation := func(verb string) authorizationv1.ResourceAttributes {
		return authorizationv1.ResourceAttributes{Group: group, Resource: "consoleclusterrevocations", Namespace: hubNS, Verb: verb}
	}
	enrollmentStatus := func() authorizationv1.ResourceAttributes {
		access := enrollment("patch", "prod-ap-3")
		access.Subresource = "status"
		return access
	}
	status := func(verb string) authorizationv1.ResourceAttributes {
		access := entry(verb, config.Registry.ThisCluster)
		access.Subresource = "status"
		return access
	}
	tests := []struct {
		name    string
		user    string
		access  authorizationv1.ResourceAttributes
		allowed bool
	}{
		{name: "the console lists the entries", user: hubSA, access: entry("list", ""), allowed: true},
		{name: "the console writes an entry's status", user: hubSA, access: status("patch"), allowed: true},
		{name: "the console changes no entry", user: hubSA, access: entry("update", config.Registry.ThisCluster)},
		{name: "the console adds no entry", user: hubSA, access: entry("create", "")},
		{name: "the console deletes no entry", user: hubSA, access: entry("delete", config.Registry.ThisCluster)},
		{name: "the console lists nothing elsewhere", user: hubSA, access: authorizationv1.ResourceAttributes{
			Group: group, Resource: "consoleclusters", Namespace: "storefront", Verb: "list"}},
		{name: "an account gets this cluster's entry", user: hubPrefix + "sam", access: entry("get", config.Registry.ThisCluster), allowed: true},
		{name: "an account gets no other entry", user: hubPrefix + "sam", access: entry("get", "prod-ap-2")},
		{name: "an account lists no entry", user: hubPrefix + "sam", access: entry("list", "")},
		{name: "an account writes no status", user: hubPrefix + "sam", access: status("patch")},
		{name: "an administrator adds an entry", user: hubPrefix + "alex", access: entry("create", ""), allowed: true},
		{name: "an administrator deletes an entry", user: hubPrefix + "alex", access: entry("delete", "prod-ap-2"), allowed: true},
		{name: "an administrator writes no status", user: hubPrefix + "alex", access: status("patch")},
		{name: "an administrator starts an enrollment", user: hubPrefix + "alex", access: enrollment("create", ""), allowed: true},
		{name: "an administrator writes no enrollment status", user: hubPrefix + "alex", access: enrollmentStatus()},
		{name: "an account starts no enrollment", user: hubPrefix + "sam", access: enrollment("create", "")},
		{name: "the console starts no enrollment", user: hubSA, access: enrollment("create", "")},
		{name: "the console records an enrollment", user: hubSA, access: enrollmentStatus(), allowed: true},
		{name: "the console removes an expired enrollment", user: hubSA, access: enrollment("delete", "prod-ap-3"), allowed: true},
		{name: "the console changes no enrollment", user: hubSA, access: enrollment("update", "prod-ap-3")},
		{name: "the console fences a connection", user: hubSA, access: revocation("create"), allowed: true},
		{name: "the console lifts no fence by deleting it", user: hubSA, access: revocation("delete")},
		{name: "an administrator reads the fences", user: hubPrefix + "alex", access: revocation("list"), allowed: true},
		{name: "an administrator writes no fence", user: hubPrefix + "alex", access: revocation("create")},
		{name: "an administrator lifts no fence by deleting it", user: hubPrefix + "alex", access: revocation("delete")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			review := &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &test.access},
			}

			c.Assert(as(t, test.user).Create(t.Context(), review), qt.IsNil)

			c.Assert(review.Status.Allowed, qt.Equals, test.allowed, qt.Commentf("%s", review.Status.Reason))
		})
	}
}
