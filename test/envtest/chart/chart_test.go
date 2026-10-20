// Package chart_test installs the chart's objects on a real API server, with
// the operator chart's own approver ClusterRole beside them, and asks the
// API server what each account and the console's ServiceAccount may do:
// what `kubectl auth can-i --list --as` answers.
package chart_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{}, &plane))
}

const (
	toolsMod       = "../../../tools/chart/go.mod"
	consoleChart   = "../../../charts/ptah-console"
	fullValues     = "../../chart/testdata/full.values.yaml"
	releaseNS      = "ptah-console"
	serviceAccount = "system:serviceaccount:ptah-console:console-ptah-console"
	// A digest that satisfies the operator chart's schema; nothing pulls it.
	digest = "sha256:f68968558f0634c1e388f2be3f8d9d0f7c5916631abe16c5bae55d21e8e909c0"
)

func helm(args ...string) (string, error) {
	command := exec.Command("go", append([]string{"tool", "-modfile=" + toolsMod, "helm"}, args...)...) // #nosec G204 -- the pinned Helm, with arguments this test writes.
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("helm %s: %w: %s", args[0], err, stderr.String())
	}
	return stdout.String(), nil
}

func decode(rendered string) ([]*unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(rendered), 4096)
	var out []*unstructured.Unstructured
	for {
		var object map[string]any
		if err := decoder.Decode(&object); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		if object != nil {
			out = append(out, &unstructured.Unstructured{Object: object})
		}
	}
}

// installed is what the chart put on the API server: the operator's approver
// ClusterRole, then every object of the console's full render the API
// server has a kind for. The HTTPRoute is a Gateway API CRD envtest does not
// serve.
var installed = sync.OnceValues(func() ([]string, error) {
	ctx := context.Background()
	listed, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/stokaro/ptah-operator").Output()
	if err != nil {
		return nil, err
	}
	operatorRBAC, err := helm("template", "ptah-operator", strings.TrimSpace(string(listed))+"/charts/ptah-operator",
		"--namespace", "ptah-system", "--show-only", "templates/rbac.yaml",
		"--set", "image.digest="+digest,
		"--set", "execution.executorImage=example.invalid/ptah@"+digest,
		"--set", "execution.runnerImage=example.invalid/runner@"+digest,
		"--set", "execution.ptahVersion=v0.13.0")
	if err != nil {
		return nil, err
	}
	operatorObjects, err := decode(operatorRBAC)
	if err != nil {
		return nil, err
	}
	approver := slices.IndexFunc(operatorObjects, func(object *unstructured.Unstructured) bool {
		return object.GetKind() == "ClusterRole" && strings.HasSuffix(object.GetName(), "-approver")
	})
	if approver < 0 {
		return nil, errors.New("the operator chart renders no approver ClusterRole")
	}
	approverRole := operatorObjects[approver]
	consoleRender, err := helm("template", "console", consoleChart, "--namespace", releaseNS,
		"--values", fullValues, "--set", "approverClusterRole="+approverRole.GetName())
	if err != nil {
		return nil, err
	}
	consoleObjects, err := decode(consoleRender)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{releaseNS, "storefront", "billing", "elsewhere"} {
		if err := plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
			return nil, err
		}
	}
	var created []string
	for _, object := range append([]*unstructured.Unstructured{approverRole}, consoleObjects...) {
		if object.GetKind() == "HTTPRoute" {
			continue
		}
		if err := plane.Admin.Create(ctx, object); err != nil {
			return nil, fmt.Errorf("create %s %s/%s: %w", object.GetKind(), object.GetNamespace(), object.GetName(), err)
		}
		created = append(created, object.GetKind()+" "+object.GetNamespace()+"/"+object.GetName())
	}
	return created, nil
})

func install(t *testing.T) []string {
	t.Helper()
	created, err := installed()
	qt.New(t).Assert(err, qt.IsNil)
	return created
}

// as is a client that acts as user, through the administrator impersonating
// it, which is how kubectl --as asks.
func as(t *testing.T, user string) *kube.Client {
	t.Helper()
	c := qt.New(t)
	config := rest.CopyConfig(plane.Env.Config)
	config.Impersonate = rest.ImpersonationConfig{UserName: user}
	client, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	return client
}

// The API server accepts every object the chart renders, at the minor
// envtest runs.
func TestTheAPIServerAcceptsTheChartsObjects(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	created := install(t)

	for _, kind := range []string{"Deployment", "Service", "ConfigMap", "ServiceAccount", "Ingress", "NetworkPolicy",
		"ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding"} {
		c.Assert(slices.ContainsFunc(created, func(object string) bool { return strings.HasPrefix(object, kind+" ") }),
			qt.IsTrue, qt.Commentf("no %s was created", kind))
	}
}

// granted lists what a user may do in a namespace, as the API server's rules
// review says, without what every authenticated user may: asking about
// itself, and the discovery and health paths.
func granted(t *testing.T, user, namespace string) []string {
	t.Helper()
	c := qt.New(t)
	review := &authorizationv1.SelfSubjectRulesReview{Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: namespace}}
	c.Assert(as(t, user).Create(t.Context(), review), qt.IsNil)
	var out []string
	for _, rule := range review.Status.ResourceRules {
		for _, group := range rule.APIGroups {
			for _, resource := range rule.Resources {
				if strings.HasPrefix(resource, "selfsubject") {
					continue
				}
				for _, verb := range rule.Verbs {
					out = append(out, fmt.Sprintf("%s/%s %s", group, resource, verb))
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func grants(groupResources map[string][]string, verbs ...string) []string {
	var out []string
	for group, resources := range groupResources {
		for _, resource := range resources {
			for _, verb := range verbs {
				out = append(out, group+"/"+resource+" "+verb)
			}
		}
	}
	return out
}

func union(parts ...[]string) []string {
	var out []string
	for _, part := range parts {
		out = append(out, part...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

var (
	read      = []string{"get", "list", "watch"}
	ptahKinds = []string{"ptahschemas", "ptahschemaplans", "ptahschemaapprovals", "ptahmigrations",
		"ptahmigrationplans", "ptahmigrationapprovals", "ptahmigrationrunacknowledgments"}
	viewer   = grants(map[string][]string{"operator.ptah.run": ptahKinds}, read...)
	approver = union(
		grants(map[string][]string{"operator.ptah.run": append(slices.Clone(ptahKinds), "ptahschemaplanchunks")}, read...),
		grants(map[string][]string{"operator.ptah.run": {"ptahschemaapprovals", "ptahmigrationapprovals", "ptahmigrationrunacknowledgments"}}, "create"))
	planReader = grants(map[string][]string{"operator.ptah.run": {"ptahschemas", "ptahschemaplans", "ptahschemaplanchunks"}}, "get")
	author     = grants(map[string][]string{"operator.ptah.run": {"ptahschemas", "ptahmigrations"}},
		"get", "list", "watch", "create", "update", "patch", "delete")
	diagnostics = union(
		grants(map[string][]string{"operator.ptah.run": ptahKinds}, read...),
		grants(map[string][]string{"batch": {"jobs"}, "": {"pods", "pods/log", "events"}}, read...))
)

// Each account may do what its roles grant in the namespaces of the
// ceiling, and nothing anywhere else.
func TestEachAccountMayDoOnlyWhatItsRolesGrant(t *testing.T) {
	t.Parallel()
	install(t)
	tests := []struct {
		account   string
		namespace string
		want      []string
	}{
		{account: "alex", namespace: "storefront", want: union(viewer, approver)},
		{account: "alex", namespace: "billing", want: union(viewer, approver)},
		{account: "sam", namespace: "storefront", want: union(author, planReader)},
		{account: "kim", namespace: "billing", want: union(diagnostics, viewer)},
		{account: "alex", namespace: "elsewhere"},
		{account: "kim", namespace: "elsewhere"},
		{account: "alex", namespace: releaseNS},
	}
	for _, test := range tests {
		t.Run(test.account+" in "+test.namespace, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			got := granted(t, "ptah-console:prod-eu-1:"+test.account, test.namespace)

			c.Assert(got, qt.DeepEquals, test.want)
		})
	}
}

// The console's ServiceAccount may impersonate the accounts' usernames and
// nothing else, and may read nothing of Ptah's itself.
func TestTheServiceAccountMayOnlyImpersonateTheAccounts(t *testing.T) {
	t.Parallel()
	install(t)
	tests := []struct {
		name    string
		access  authorizationv1.ResourceAttributes
		allowed bool
	}{
		{name: "an account", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "users", Name: "ptah-console:prod-eu-1:alex"}, allowed: true},
		{name: "a username no account has", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "users", Name: "ptah-console:prod-eu-1:lee"}},
		{name: "system:masters", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "groups", Name: "system:masters"}},
		{name: "a ServiceAccount", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "serviceaccounts", Namespace: "kube-system", Name: "default"}},
		{name: "a UID", access: authorizationv1.ResourceAttributes{
			Group: "authentication.k8s.io", Verb: "impersonate", Resource: "uids", Name: "1"}},
		{name: "schemas, itself", access: authorizationv1.ResourceAttributes{
			Group: "operator.ptah.run", Verb: "list", Resource: "ptahschemas", Namespace: "storefront"}},
		{name: "a Secret, itself", access: authorizationv1.ResourceAttributes{
			Verb: "get", Resource: "secrets", Namespace: releaseNS, Name: "ptah-console-passwords"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			review := &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &test.access},
			}

			c.Assert(as(t, serviceAccount).Create(t.Context(), review), qt.IsNil)

			c.Assert(review.Status.Allowed, qt.Equals, test.allowed, qt.Commentf("%s", review.Status.Reason))
		})
	}
}
