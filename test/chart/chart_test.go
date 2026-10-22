// Package chart_test renders charts/ptah-console with the Helm the repository
// pins (tools/chart) and holds the result to golden files, to the grants it
// may make, and to every refusal the values schema and templates promise.
package chart_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

var update = flag.Bool("update", false, "rewrite the golden renders")

const (
	chartDir  = "../../charts/ptah-console"
	toolsMod  = "../../tools/chart/go.mod"
	namespace = "ptah-console"
)

// helm runs the pinned Helm.
func helm(args ...string) (string, error) {
	command := exec.Command("go", append([]string{"tool", "-modfile=" + toolsMod, "helm"}, args...)...) // #nosec G204 -- the pinned Helm, with arguments this test writes.
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// operatorDir is the operator module go.mod requires, downloaded first: go
// list -m names no directory for a module nothing has downloaded yet.
func operatorDir() (string, error) {
	downloaded, err := exec.Command("go", "mod", "download", "-json", "github.com/stokaro/ptah-operator").Output()
	if err != nil {
		return "", fmt.Errorf("download the operator module: %w", err)
	}
	var module struct{ Dir string }
	if err := json.Unmarshal(downloaded, &module); err != nil || module.Dir == "" {
		return "", fmt.Errorf("download the operator module: %q", downloaded)
	}
	return module.Dir, nil
}

// render renders the chart with the value files given, in order.
func render(valueFiles ...string) (string, error) {
	args := []string{"template", "console", chartDir, "--namespace", namespace}
	for _, file := range valueFiles {
		args = append(args, "--values", file)
	}
	return helm(args...)
}

func mustRender(t *testing.T, valueFiles ...string) string {
	t.Helper()
	rendered, err := render(valueFiles...)
	qt.New(t).Assert(err, qt.IsNil)
	return rendered
}

func values(name string) string { return filepath.Join("testdata", name+".values.yaml") }

// objects are the rendered documents, decoded.
func objects(t *testing.T, rendered string) []unstructured.Unstructured {
	t.Helper()
	c := qt.New(t)
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(rendered), 4096)
	var out []unstructured.Unstructured
	for {
		var object map[string]any
		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			break
		}
		c.Assert(err, qt.IsNil)
		if object != nil {
			out = append(out, unstructured.Unstructured{Object: object})
		}
	}
	c.Assert(out, qt.Not(qt.HasLen), 0)
	return out
}

func ofKind(all []unstructured.Unstructured, kind string) []unstructured.Unstructured {
	return slices.DeleteFunc(slices.Clone(all), func(object unstructured.Unstructured) bool { return object.GetKind() != kind })
}

func convert[T any](t *testing.T, object unstructured.Unstructured) T {
	t.Helper()
	var typed T
	qt.New(t).Assert(runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &typed), qt.IsNil)
	return typed
}

// golden is the file's content, after writing rendered into it when the
// test runs with -update.
func golden(t *testing.T, path, rendered string) string {
	t.Helper()
	c := qt.New(t)
	if *update {
		c.Assert(os.WriteFile(path, []byte(rendered), 0o600), qt.IsNil)
	}
	want, err := os.ReadFile(path)
	c.Assert(err, qt.IsNil)
	return string(want)
}

func TestRendersMatchTheirGoldenFiles(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"minimal", "full", "registry", "agents"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			rendered := mustRender(t, values(name))

			want := golden(t, filepath.Join("testdata", name+".golden.yaml"), rendered)

			c.Assert(rendered, qt.Equals, want, qt.Commentf("run go test ./test/chart -update and review the diff"))
		})
	}
}

func TestTheChartLints(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	_, err := helm("lint", "--strict", chartDir, "--values", values("minimal"))

	c.Assert(err, qt.IsNil)
}

// No rule the chart renders reaches past what it names: no wildcard in any
// group, resource, verb or name.
func TestNoRenderedRuleHasAWildcard(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"minimal", "full", "registry", "agents"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			all := objects(t, mustRender(t, values(name)))
			var rules []rbacv1.PolicyRule
			for _, role := range ofKind(all, "Role") {
				rules = append(rules, convert[rbacv1.Role](t, role).Rules...)
			}
			for _, role := range ofKind(all, "ClusterRole") {
				rules = append(rules, convert[rbacv1.ClusterRole](t, role).Rules...)
			}

			var wildcards []string
			for _, rule := range rules {
				for _, field := range [][]string{rule.APIGroups, rule.Resources, rule.Verbs, rule.ResourceNames, rule.NonResourceURLs} {
					wildcards = append(wildcards, slices.DeleteFunc(slices.Clone(field), func(v string) bool { return !strings.Contains(v, "*") })...)
				}
			}

			c.Assert(len(rules) > 1, qt.IsTrue, qt.Commentf("examined %d rules", len(rules)))
			c.Assert(wildcards, qt.HasLen, 0)
		})
	}
}

// The ServiceAccount may impersonate exactly the usernames the console
// builds from the configuration it is given, which the console's own parser
// accepts.
func TestTheConsoleImpersonatesExactlyItsAccounts(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRender(t, values("full")))

	configMaps := ofKind(all, "ConfigMap")
	c.Assert(configMaps, qt.HasLen, 1)
	text, _, _ := unstructured.NestedString(configMaps[0].Object, "data", "configuration.yaml")
	config, err := accounts.ParseConfig([]byte(text))
	c.Assert(err, qt.IsNil)
	c.Assert(config.TrustedProxies, qt.DeepEquals, []string{"10.244.0.0/16", "fd00:10::/64"})
	var usernames []string
	for _, account := range config.Accounts {
		usernames = append(usernames, config.Username(account))
	}

	clusterRoles := ofKind(all, "ClusterRole")
	c.Assert(clusterRoles, qt.HasLen, 1)
	c.Assert(convert[rbacv1.ClusterRole](t, clusterRoles[0]).Rules, qt.DeepEquals, []rbacv1.PolicyRule{{
		APIGroups: []string{""}, Resources: []string{"users"}, Verbs: []string{"impersonate"}, ResourceNames: usernames,
	}})
	c.Assert(usernames, qt.DeepEquals, []string{
		"ptah-console:prod-eu-1:alex", "ptah-console:prod-eu-1:sam", "ptah-console:prod-eu-1:kim",
	})
	bindings := ofKind(all, "ClusterRoleBinding")
	c.Assert(bindings, qt.HasLen, 1)
	binding := convert[rbacv1.ClusterRoleBinding](t, bindings[0])
	c.Assert(binding.RoleRef.Name, qt.Equals, clusterRoles[0].GetName())
	c.Assert(binding.Subjects, qt.DeepEquals, []rbacv1.Subject{
		{Kind: "ServiceAccount", Name: "console-ptah-console", Namespace: namespace},
	})
}

// Each account is bound to the roles it was given, in every namespace of the
// ceiling and in no other.
func TestEachAccountGetsItsRolesInEveryNamespace(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	all := objects(t, mustRender(t, values("full")))
	bound := make(map[string][]string)
	for _, object := range ofKind(all, "RoleBinding") {
		binding := convert[rbacv1.RoleBinding](t, object)
		for _, subject := range binding.Subjects {
			key := binding.Namespace + " " + binding.RoleRef.Kind + "/" + binding.RoleRef.Name
			bound[key] = append(bound[key], strings.TrimPrefix(subject.Name, "ptah-console:prod-eu-1:"))
		}
	}

	want := make(map[string][]string)
	for _, ns := range []string{"storefront", "billing"} {
		want[ns+" Role/console-ptah-console-viewer"] = []string{"alex", "kim"}
		want[ns+" Role/console-ptah-console-plan-reader"] = []string{"sam"}
		want[ns+" Role/console-ptah-console-desired-state-author"] = []string{"sam"}
		want[ns+" Role/console-ptah-console-diagnostic-reader"] = []string{"kim"}
		want[ns+" ClusterRole/ptah-operator-ptah-operator-approver"] = []string{"alex"}
	}
	c.Assert(bound, qt.DeepEquals, want)
}

// The chart installs on the Kubernetes minors the operator supports, and
// says so in the constraint Helm checks.
func TestTheChartsSupportTheOperatorsKubernetesWindow(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	dir, err := operatorDir()
	c.Assert(err, qt.IsNil)
	data, err := os.ReadFile(filepath.Join(dir, "support", "kubernetes.json"))
	c.Assert(err, qt.IsNil)
	var support struct {
		Releases []struct{ Minor string } `json:"releases"`
	}
	c.Assert(json.Unmarshal(data, &support), qt.IsNil)
	c.Assert(support.Releases, qt.Not(qt.HasLen), 0)
	first, last := support.Releases[0].Minor, support.Releases[len(support.Releases)-1].Minor
	var lastMajor, lastMinor int
	_, err = fmt.Sscanf(last, "%d.%d", &lastMajor, &lastMinor)
	c.Assert(err, qt.IsNil)

	for _, dir := range []string{chartDir, agentChartDir} {
		chart, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
		c.Assert(err, qt.IsNil)
		var metadata struct {
			KubeVersion string `json:"kubeVersion"`
		}
		c.Assert(yaml.Unmarshal(chart, &metadata), qt.IsNil)

		c.Assert(metadata.KubeVersion, qt.Equals, fmt.Sprintf(">=%s.0-0 <%d.%d.0-0", first, lastMajor, lastMinor+1), qt.Commentf("%s", dir))
	}
}

func TestEachRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		overlay string
		says    string
	}{
		{name: "no account", overlay: "accounts: []", says: `/accounts.*minItems`},
		{name: "a wildcard namespace", overlay: `namespaces: ["*"]`, says: `/namespaces/0.*does not match pattern`},
		{name: "a wildcard role", overlay: "accounts: [{id: alex, login: alex@example.com, roles: ['*']}]",
			says: `/accounts/0/roles/0.*value must be one of`},
		{name: "a system: account id", overlay: "accounts: [{id: 'system:masters', login: a@example.com, roles: [viewer]}]",
			says: `/accounts/0/id.*does not match pattern`},
		{name: "a system: login", overlay: "accounts: [{id: alex, login: 'System:admin', roles: [viewer]}]",
			says: `/accounts/0/login.*'not' failed`},
		{name: "an administrator as the approver role", overlay: "approverClusterRole: cluster-admin",
			says: `/approverClusterRole.*'not' failed`},
		{name: "a system: role as the approver role", overlay: "approverClusterRole: 'system:aggregate-to-admin'",
			says: `/approverClusterRole.*'not' failed`},
		{name: "two replicas", overlay: "replicaCount: 2", says: `/replicaCount.*maximum`},
		{name: "a pull secret given as a bare name", overlay: "imagePullSecrets: [ghcr-pull]",
			says: `/imagePullSecrets/0.*got string, want object`},
		{name: "the same id twice",
			overlay: "accounts: [{id: alex, login: a@example.com, roles: [viewer]}, {id: alex, login: b@example.com, roles: [viewer]}]",
			says:    `accounts: the id alex is given twice`},
		{name: "the same login twice in another case",
			overlay: "accounts: [{id: a, login: alex@example.com, roles: [viewer]}, {id: b, login: Alex@Example.com, roles: [viewer]}]",
			says:    `accounts: the login Alex@Example.com is given twice, ignoring case`},
		{name: "both TLS choices", overlay: "tls: {terminatedUpstream: true}",
			says: `tls: give secretName or terminatedUpstream, not both`},
		{name: "no TLS at all", overlay: "tls: {secretName: ''}",
			says: `tls: a hosted console serves HTTPS only`},
		{name: "no passwords Secret", overlay: "passwords: {secretName: ''}", says: `/passwords/secretName.*minLength`},
		{name: "a public URL over HTTP", overlay: "publicURL: http://console.example.com", says: `/publicURL.*does not match pattern`},
		{name: "a public URL with a path", overlay: "publicURL: https://example.com/console", says: `/publicURL.*does not match pattern`},
		{name: "a policy that does not name the API server", overlay: "networkPolicy: {enabled: true}",
			says: `networkPolicy: apiServer.cidrs names the API server`},
		{name: "a value the chart does not read", overlay: "replicas: 3", says: `additional properties 'replicas' not allowed`},
		{name: "trusted proxies without the policy", overlay: "trustedProxies: [10.244.0.0/16]",
			says: `trustedProxies: needs networkPolicy.enabled, so only the proxies can reach the console`},
		{name: "trusted proxies with a policy that names no peer",
			overlay: "trustedProxies: [10.244.0.0/16]\nnetworkPolicy: {enabled: true, apiServer: {cidrs: [10.96.0.1/32]}}",
			says:    `trustedProxies: needs networkPolicy.ingressFrom to name the proxies`},
		{name: "every IPv4 address as a proxy", overlay: "trustedProxies: [0.0.0.0/0]", says: `/trustedProxies/0.*'not' failed`},
		{name: "every IPv6 address as a proxy", overlay: "trustedProxies: ['::/0']", says: `/trustedProxies/0.*'not' failed`},
		{name: "a proxy that is no network", overlay: "trustedProxies: [10.244.0.0]", says: `/trustedProxies/0.*does not match pattern`},
		{name: "a registry value the chart does not read", overlay: "registry: {enabled: true, connectionUID: x}",
			says: `/registry.*additional properties 'connectionUID' not allowed`},
		{name: "agents without the registry", overlay: "agents: {enabled: true, tls: {secretName: agents-tls}}",
			says: `agents: serves the agents of the registry's connections; enable the registry`},
		{name: "agents without their certificate", overlay: "registry: {enabled: true}\nagents: {enabled: true}",
			says: `agents: tls.secretName names the agent listener's certificate`},
		{name: "agents without what an enrollment tells them",
			overlay: "registry: {enabled: true}\nagents: {enabled: true, tls: {secretName: agents-tls}, address: 'https://agents.example.com'}",
			says:    `agents: address, serverName and caBundle are what an enrollment file tells agents`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			// Helm splits --values on commas, so the path holds none.
			overlay := filepath.Join(t.TempDir(), "overlay.yaml")
			c.Assert(overlay, qt.Not(qt.Contains), ",")
			c.Assert(os.WriteFile(overlay, []byte(test.overlay+"\n"), 0o600), qt.IsNil)

			rendered, err := render(values("minimal"), overlay)

			c.Assert(rendered, qt.Equals, "")
			c.Assert(err, qt.ErrorMatches, `(?s).*`+test.says+`.*`)
		})
	}
}
