package chart_test

import (
	"os"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

func inNamespace(all []unstructured.Unstructured, name string) []unstructured.Unstructured {
	var out []unstructured.Unstructured
	for _, object := range all {
		if object.GetNamespace() == name {
			out = append(out, object)
		}
	}
	return out
}

// The registry's grants keep trust and visibility apart: the console's own
// identity lists entries and writes their status and does nothing else,
// connection administrators manage entries, and every other account may get
// this cluster's entry by name and list none, so it learns of no other.
func TestTheRegistryGrantsAreSplit(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRender(t, values("registry")))

	entries := ofKind(all, "ConsoleCluster")
	c.Assert(entries, qt.HasLen, 1)
	entry := convert[consolev1alpha1.ConsoleCluster](t, entries[0])
	c.Assert(entry.Namespace, qt.Equals, namespace)
	c.Assert(entry.Spec.Profile, qt.Equals, consolev1alpha1.ProfileCoLocated)
	c.Assert(entry.Spec.InstallationID, qt.Equals, "prod-eu-1")
	c.Assert(entry.Spec.Namespaces, qt.DeepEquals, []string{"storefront", "billing"})
	c.Assert(entry.Name, qt.Equals, "this-cluster-"+entry.Spec.ConnectionUID[:8])

	// The console checks itself against the same entry it is told about.
	configMaps := ofKind(all, "ConfigMap")
	text, _, _ := unstructured.NestedString(configMaps[0].Object, "data", "configuration.yaml")
	config, err := accounts.ParseConfig([]byte(text))
	c.Assert(err, qt.IsNil)
	c.Assert(config.Registry, qt.DeepEquals, &accounts.Registry{
		Namespace: namespace, ThisCluster: entry.Name, ConnectionUID: entry.Spec.ConnectionUID,
	})

	rules := make(map[string][]rbacv1.PolicyRule)
	for _, object := range inNamespace(ofKind(all, "Role"), namespace) {
		role := convert[rbacv1.Role](t, object)
		rules[role.Name] = role.Rules
	}
	group := consolev1alpha1.GroupVersion.Group
	c.Assert(rules, qt.DeepEquals, map[string][]rbacv1.PolicyRule{
		"console-ptah-console-registry-housekeeping": {
			{APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterpreferences"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{group}, Resources: []string{"consoleclusters/status"}, Verbs: []string{"update", "patch"}},
			{APIGroups: []string{group}, Resources: []string{"consoleclusterenrollments"}, Verbs: []string{"get", "list", "watch", "delete"}},
			{APIGroups: []string{group}, Resources: []string{"consoleclusterenrollments/status"}, Verbs: []string{"update", "patch"}},
			{APIGroups: []string{group}, Resources: []string{"consoleclusterrevocations"}, Verbs: []string{"get", "list", "watch", "create"}},
		},
		"console-ptah-console-registry-this-cluster": {
			{APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterpreferences"}, ResourceNames: []string{entry.Name}, Verbs: []string{"get"}},
		},
		"console-ptah-console-registry-admin": {
			{
				APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterpreferences", "consoleclusterenrollments"},
				Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{APIGroups: []string{group}, Resources: []string{"consoleclusterrevocations"}, Verbs: []string{"get", "list", "watch"}},
		},
	})
	subjects := make(map[string][]string)
	for _, object := range inNamespace(ofKind(all, "RoleBinding"), namespace) {
		binding := convert[rbacv1.RoleBinding](t, object)
		for _, subject := range binding.Subjects {
			subjects[binding.RoleRef.Name] = append(subjects[binding.RoleRef.Name], subject.Kind+"/"+subject.Name)
		}
	}
	c.Assert(subjects, qt.DeepEquals, map[string][]string{
		"console-ptah-console-registry-housekeeping": {"ServiceAccount/console-ptah-console"},
		"console-ptah-console-registry-this-cluster": {"User/ptah-console:prod-eu-1:alex", "User/ptah-console:prod-eu-1:sam"},
		"console-ptah-console-registry-admin":        {"User/ptah-console:prod-eu-1:alex"},
	})
	// connection-admin grants nothing in the namespaces of the ceiling.
	for _, object := range ofKind(all, "RoleBinding") {
		c.Assert(object.GetName(), qt.Not(qt.Contains), "connection-admin")
	}
}

// The connection UID follows from the installation and the ceiling: the order
// the namespaces are listed in is not part of it, and another ceiling is
// another connection. Without the registry nothing of it renders.
func TestThisClustersConnectionFollowsItsScope(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	uid := func(overlay string) string {
		file := filepath.Join(t.TempDir(), "overlay.yaml")
		c.Assert(os.WriteFile(file, []byte(overlay+"\n"), 0o600), qt.IsNil)
		entries := ofKind(objects(t, mustRender(t, values("registry"), file)), "ConsoleCluster")
		c.Assert(entries, qt.HasLen, 1)
		return convert[consolev1alpha1.ConsoleCluster](t, entries[0]).Spec.ConnectionUID
	}
	base := uid("namespaces: [storefront, billing]")
	c.Assert(uid("namespaces: [billing, storefront]"), qt.Equals, base)
	c.Assert(uid("namespaces: [storefront, billing, payments]"), qt.Not(qt.Equals), base)
	c.Assert(uid("installationID: prod-us-1"), qt.Not(qt.Equals), base)

	without := objects(t, mustRender(t, values("minimal")))
	c.Assert(ofKind(without, "ConsoleCluster"), qt.HasLen, 0)
	for _, object := range ofKind(without, "Role") {
		c.Assert(object.GetName(), qt.Not(qt.Contains), "registry")
	}
}

// The agents have a Service of their own, which carries the agent port and
// nothing else, and the browser's Service does not carry the agent port.
func TestAgentsHaveAServiceOfTheirOwn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRender(t, values("agents")))

	names := make(map[string]string)
	for _, object := range ofKind(all, "Service") {
		service := convert[corev1.Service](t, object)
		for _, port := range service.Spec.Ports {
			names[service.Name] += port.TargetPort.StrVal + " "
		}
	}
	c.Assert(names, qt.DeepEquals, map[string]string{
		"console-ptah-console":        "https ",
		"console-ptah-console-agents": "agents ",
	})
	deployment := convert[appsv1.Deployment](t, ofKind(all, "Deployment")[0])
	container := deployment.Spec.Template.Spec.Containers[0]
	c.Assert(container.Ports, qt.HasLen, 2)
	c.Assert(container.Args, qt.Contains, "--agent-listen=:8444")
	policy := ofKind(all, "NetworkPolicy")[0]
	ingress, _, _ := unstructured.NestedSlice(policy.Object, "spec", "ingress")
	c.Assert(ingress, qt.HasLen, 2)
	agentsRule, _ := ingress[1].(map[string]any)
	_, restricted := agentsRule["from"]
	c.Assert(restricted, qt.IsFalse, qt.Commentf("agents come from other clusters; an empty agents.ingressFrom admits any source"))
}

// The configuration the chart writes with agents on is one the console
// accepts, and tells it what an enrollment file gives agents.
func TestTheConsoleAcceptsTheAgentsConfiguration(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRender(t, values("agents")))
	text, _, _ := unstructured.NestedString(ofKind(all, "ConfigMap")[0].Object, "data", "configuration.yaml")

	config, err := accounts.ParseConfig([]byte(text))

	c.Assert(err, qt.IsNil)
	c.Assert(config.Registry.Agents, qt.IsNotNil)
	c.Assert(config.Registry.Agents.Address, qt.Equals, "https://agents.console.example.com")
	c.Assert(config.Registry.Agents.CABundle, qt.Contains, "-----BEGIN CERTIFICATE-----")
}
