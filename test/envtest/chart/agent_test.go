package chart_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	agentChart  = "../../../charts/ptah-console-agent"
	agentValues = "../../chart/testdata/agent/full.values.yaml"
	agentNS     = "ptah-console-agent"
	agentSA     = "system:serviceaccount:" + agentNS + ":agent-ptah-console-agent"
)

// installedAgent is the agent chart's full render on the API server.
var installedAgent = sync.OnceValue(func() error {
	ctx := context.Background()
	rendered, err := helm("template", "agent", agentChart, "--namespace", agentNS, "--values", agentValues)
	if err != nil {
		return err
	}
	objects, err := decode(rendered)
	if err != nil {
		return err
	}
	if err := plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: agentNS}}); err != nil {
		return err
	}
	for _, object := range objects {
		if err := plane.Admin.Create(ctx, object); err != nil {
			return fmt.Errorf("create %s %s/%s: %w", object.GetKind(), object.GetNamespace(), object.GetName(), err)
		}
	}
	return nil
})

// The agent's ServiceAccount may impersonate the allowlisted accounts'
// usernames and do nothing else: no other identity, no Secret, not even its
// own credential, no RBAC object, and nothing of Ptah's itself.
func TestTheAgentMayOnlyImpersonateItsAccounts(t *testing.T) {
	t.Parallel()
	qt.New(t).Assert(installedAgent(), qt.IsNil)
	tests := []struct {
		name    string
		access  authorizationv1.ResourceAttributes
		allowed bool
	}{
		{name: "an allowlisted account", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "users", Name: "ptah-console:prod:alex"}, allowed: true},
		{name: "another allowlisted account", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "users", Name: "ptah-console:prod:sam"}, allowed: true},
		{name: "an account of another installation", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "users", Name: "ptah-console:other:alex"}},
		{name: "system:masters", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "groups", Name: "system:masters"}},
		{name: "a ServiceAccount", access: authorizationv1.ResourceAttributes{
			Verb: "impersonate", Resource: "serviceaccounts", Namespace: "kube-system", Name: "default"}},
		{name: "its own credential", access: authorizationv1.ResourceAttributes{
			Verb: "get", Resource: "secrets", Namespace: agentNS, Name: "ptah-console-agent-credential"}},
		{name: "a change to its own credential", access: authorizationv1.ResourceAttributes{
			Verb: "update", Resource: "secrets", Namespace: agentNS, Name: "ptah-console-agent-credential"}},
		{name: "a RoleBinding", access: authorizationv1.ResourceAttributes{
			Group: "rbac.authorization.k8s.io", Verb: "create", Resource: "rolebindings", Namespace: "storefront"}},
		{name: "schemas, itself", access: authorizationv1.ResourceAttributes{
			Group: "operator.ptah.run", Verb: "list", Resource: "ptahschemas", Namespace: "storefront"}},
		{name: "result records, itself", access: authorizationv1.ResourceAttributes{
			Group: "operator.ptah.run", Verb: "list", Resource: "ptahresultrecords", Namespace: "storefront"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			review := &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &test.access},
			}

			c.Assert(as(t, agentSA).Create(t.Context(), review), qt.IsNil)

			c.Assert(review.Status.Allowed, qt.Equals, test.allowed, qt.Commentf("%s", review.Status.Reason))
		})
	}
}
