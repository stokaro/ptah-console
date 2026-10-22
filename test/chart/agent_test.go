package chart_test

import (
	"os"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/stokaro/ptah-console/internal/agent"
)

const (
	agentChartDir  = "../../charts/ptah-console-agent"
	agentNamespace = "ptah-console-agent"
)

func agentValues(name string) string {
	return filepath.Join("testdata", "agent", name+".values.yaml")
}

// renderAgent renders the agent chart with the value files given, in order.
func renderAgent(valueFiles ...string) (string, error) {
	args := []string{"template", "agent", agentChartDir, "--namespace", agentNamespace}
	for _, file := range valueFiles {
		args = append(args, "--values", file)
	}
	return helm(args...)
}

func mustRenderAgent(t *testing.T, valueFiles ...string) string {
	t.Helper()
	rendered, err := renderAgent(valueFiles...)
	qt.New(t).Assert(err, qt.IsNil)
	return rendered
}

// overlay is a values file holding text, for one test.
func overlay(t *testing.T, text string) string {
	t.Helper()
	c := qt.New(t)
	// Helm splits --values on commas, so the path holds none.
	path := filepath.Join(t.TempDir(), "overlay.yaml")
	c.Assert(path, qt.Not(qt.Contains), ",")
	c.Assert(os.WriteFile(path, []byte(text+"\n"), 0o600), qt.IsNil)
	return path
}

func TestAgentRendersMatchTheirGoldenFiles(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"minimal", "full"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			rendered := mustRenderAgent(t, agentValues(name))

			want := golden(t, filepath.Join("testdata", "agent", name+".golden.yaml"), rendered)

			c.Assert(rendered, qt.Equals, want, qt.Commentf("run go test ./test/chart -update and review the diff"))
		})
	}
}

func TestTheAgentChartLints(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	_, err := helm("lint", "--strict", agentChartDir, "--values", agentValues("minimal"))

	c.Assert(err, qt.IsNil)
}

// The agent's one grant is impersonation of exactly the usernames the agent
// itself builds for its allowlist, and the configuration the chart writes is
// one the agent accepts.
func TestTheAgentChartAndTheAgentAgree(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRenderAgent(t, agentValues("full")))

	configMaps := make(map[string]map[string]string)
	for _, object := range ofKind(all, "ConfigMap") {
		data, _, _ := unstructured.NestedStringMap(object.Object, "data")
		configMaps[object.GetName()] = data
	}
	config, err := agent.ParseConfiguration([]byte(configMaps["agent-ptah-console-agent"]["agent.yaml"]))
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte(configMaps["agent-ptah-console-agent-accounts"]["accounts.yaml"]))
	c.Assert(err, qt.IsNil)
	c.Assert(list.Accounts, qt.DeepEquals, []string{"alex", "sam"})
	c.Assert(configMaps["agent-ptah-console-agent"]["console-ca.pem"], qt.Matches, `(?s)-----BEGIN CERTIFICATE-----\n.*-----END CERTIFICATE-----\n`)

	var rules []rbacv1.PolicyRule
	for _, object := range ofKind(all, "ClusterRole") {
		rules = append(rules, convert[rbacv1.ClusterRole](t, object).Rules...)
	}
	c.Assert(rules, qt.DeepEquals, []rbacv1.PolicyRule{{
		APIGroups: []string{""}, Resources: []string{"users"}, Verbs: []string{"impersonate"},
		ResourceNames: []string{config.Username("alex"), config.Username("sam")},
	}})
	c.Assert(ofKind(all, "Role"), qt.HasLen, 0)
	c.Assert(ofKind(all, "RoleBinding"), qt.HasLen, 0)
	c.Assert(ofKind(all, "ClusterRoleBinding"), qt.HasLen, 1)
}

// Nothing listens: no Service, no Ingress, no container port, and the
// NetworkPolicy admits nothing in.
func TestNothingReachesTheAgent(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRenderAgent(t, agentValues("full")))

	for _, kind := range []string{"Service", "Ingress", "HTTPRoute"} {
		c.Assert(ofKind(all, kind), qt.HasLen, 0, qt.Commentf("%s", kind))
	}
	deployments := ofKind(all, "Deployment")
	c.Assert(deployments, qt.HasLen, 1)
	containers := convert[appsv1.Deployment](t, deployments[0]).Spec.Template.Spec.Containers
	c.Assert(containers, qt.HasLen, 1)
	c.Assert(containers[0].Ports, qt.HasLen, 0)
	policies := ofKind(all, "NetworkPolicy")
	c.Assert(policies, qt.HasLen, 1)
	ingress, found, _ := unstructured.NestedSlice(policies[0].Object, "spec", "ingress")
	types, _, _ := unstructured.NestedStringSlice(policies[0].Object, "spec", "policyTypes")
	c.Assert(found, qt.IsFalse, qt.Commentf("ingress rules: %v", ingress))
	c.Assert(types, qt.Contains, "Ingress")
}

// A changed allowlist reaches the running agent without a rollout; a change
// to what fixes the connection rolls it.
func TestOnlyTheConnectionRollsTheAgent(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	template := func(extra ...string) string {
		all := objects(t, mustRenderAgent(t, append([]string{agentValues("minimal")}, extra...)...))
		deployment := convert[appsv1.Deployment](t, ofKind(all, "Deployment")[0])
		return deployment.Spec.Template.Annotations["checksum/configuration"]
	}
	base := template()

	c.Assert(template(overlay(t, "accounts: [alex]")), qt.Equals, base)
	c.Assert(template(overlay(t, "namespaces: [storefront]")), qt.Not(qt.Equals), base)
	c.Assert(template(overlay(t, "connection: {uid: 0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d, generation: 2}")), qt.Not(qt.Equals), base)
}

func TestEachAgentChartRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		overlay string
		says    string
	}{
		{name: "no account", overlay: "accounts: []", says: `/accounts.*minItems`},
		{name: "a system name as an account", overlay: "accounts: ['system:masters']", says: `/accounts/0.*does not match pattern`},
		{name: "a username as an account", overlay: "accounts: ['ptah-console:prod:alex']", says: `/accounts/0.*does not match pattern`},
		{name: "an account twice", overlay: "accounts: [alex, alex]", says: `/accounts.*items at 0 and 1 are equal`},
		{name: "a wildcard namespace", overlay: `namespaces: ["*"]`, says: `/namespaces/0.*does not match pattern`},
		{name: "no namespace", overlay: "namespaces: []", says: `/namespaces.*minItems`},
		{name: "a console over HTTP", overlay: "console: {address: 'http://agents.console.example.com'}", says: `/console/address.*does not match pattern`},
		{name: "a console address with a path", overlay: "console: {address: 'https://console.example.com/agents'}", says: `/console/address.*does not match pattern`},
		{name: "a CA that is not PEM", overlay: "console: {caBundle: 'trust me'}", says: `/console/caBundle.*does not match pattern`},
		{name: "a connection UID in capitals", overlay: "connection: {uid: 0B9A8C7D-6E5F-4A3B-8C2D-1E0F9A8B7C6D}", says: `/connection/uid.*does not match pattern`},
		{name: "generation zero", overlay: "connection: {generation: 0}", says: `/connection/generation.*minimum`},
		{name: "no credential Secret", overlay: "credential: {secretName: ''}", says: `/credential/secretName.*minLength`},
		{name: "a policy that does not name the API server", overlay: "networkPolicy: {enabled: true, console: {cidrs: [203.0.113.10/32]}}",
			says: `networkPolicy: apiServer.cidrs names the API server`},
		{name: "a policy that does not name the console", overlay: "networkPolicy: {enabled: true, apiServer: {cidrs: [10.96.0.1/32]}}",
			says: `networkPolicy: console.cidrs names the console's listener`},
		{name: "a second replica", overlay: "replicaCount: 2", says: `additional properties 'replicaCount' not allowed`},
		{name: "groups beside the accounts", overlay: "groups: [system:masters]", says: `additional properties 'groups' not allowed`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			rendered, err := renderAgent(agentValues("minimal"), overlay(t, test.overlay))

			c.Assert(rendered, qt.Equals, "")
			c.Assert(err, qt.ErrorMatches, `(?s).*`+test.says+`.*`)
		})
	}
}
