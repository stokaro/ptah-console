package api

import (
	"context"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

const operatorGroup = "operator.ptah.run"

// ScopeService serves ScopeService.
type ScopeService struct {
	consolev1connect.UnimplementedScopeServiceHandler

	factory kube.ClientFactory
	scope   Scope
}

// NewScopeService returns the ScopeService.
func NewScopeService(factory kube.ClientFactory, scope Scope) *ScopeService {
	return &ScopeService{factory: factory, scope: scope}
}

// capabilityChecks are the questions asked for every namespace, in the order
// capabilities() reads the answers back.
func capabilityChecks(namespace string) []kube.Check {
	return []kube.Check{
		{Namespace: namespace, Verb: "list", Group: operatorGroup, Resource: "ptahschemas"},
		{Namespace: namespace, Verb: "list", Group: operatorGroup, Resource: "ptahmigrations"},
		{Namespace: namespace, Verb: "get", Group: operatorGroup, Resource: "ptahschemaplanchunks"},
		{Namespace: namespace, Verb: "create", Group: operatorGroup, Resource: "ptahschemaapprovals"},
		{Namespace: namespace, Verb: "create", Group: operatorGroup, Resource: "ptahmigrationapprovals"},
		{Namespace: namespace, Verb: "create", Group: operatorGroup, Resource: "ptahmigrationrunacknowledgments"},
		{Namespace: namespace, Verb: "patch", Group: operatorGroup, Resource: "ptahschemas"},
		{Namespace: namespace, Verb: "patch", Group: operatorGroup, Resource: "ptahmigrations"},
		{Namespace: namespace, Verb: "list", Group: operatorGroup, Resource: "ptahschemaapprovals"},
		{Namespace: namespace, Verb: "get", Group: "batch", Resource: "jobs"},
		{Namespace: namespace, Verb: "list", Resource: "pods"},
		{Namespace: namespace, Verb: "get", Resource: "pods", Subresource: "log"},
		{Namespace: namespace, Verb: "list", Resource: "events"},
	}
}

func capabilities(hints []kube.Hint) *consolev1.Capabilities {
	a := func(i int) consolev1.Answer { return answer(hints[i].Answer) }
	return &consolev1.Capabilities{
		ListSchemas: a(0), ListMigrations: a(1), ReadPlanSql: a(2),
		ApproveSchemaPlans: a(3), ApproveMigrationPlans: a(4), AcknowledgeRuns: a(5),
		SuspendSchemas: a(6), SuspendMigrations: a(7), ListDecisions: a(8),
		ReadJobs: a(9), ReadPods: a(10), ReadLogs: a(11), ReadEvents: a(12),
	}
}

func answer(a kube.Answer) consolev1.Answer {
	switch a {
	case kube.Allowed:
		return consolev1.Answer_ANSWER_ALLOWED
	case kube.Denied:
		return consolev1.Answer_ANSWER_DENIED
	default:
		return consolev1.Answer_ANSWER_UNKNOWN
	}
}

// ListNamespaces returns the candidate namespaces, or the namespaces the
// identity may list when there are none, with what the identity may do in
// each. Listing namespaces is never required.
func (s *ScopeService) ListNamespaces(ctx context.Context, _ *consolev1.ListNamespacesRequest) (*consolev1.ListNamespacesResponse, error) {
	c, _, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	names := append([]string(nil), s.scope.Candidates...)
	if len(names) == 0 {
		names = listable(ctx, c, s.scope)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) > maxNamespaces {
		names = names[:maxNamespaces]
	}

	out := &consolev1.ListNamespacesResponse{OperatorApi: operatorAPI(c.Config)}
	for _, name := range names {
		hints := kube.Can(ctx, c, capabilityChecks(name)...)
		caps := capabilities(hints)
		scope := &consolev1.NamespaceScope{Name: name, Capabilities: caps}
		switch {
		case caps.GetListSchemas() == consolev1.Answer_ANSWER_ALLOWED || caps.GetListMigrations() == consolev1.Answer_ANSWER_ALLOWED:
			scope.Access = consolev1.Access_ACCESS_OK
		case caps.GetListSchemas() == consolev1.Answer_ANSWER_DENIED && caps.GetListMigrations() == consolev1.Answer_ANSWER_DENIED:
			scope.Access = consolev1.Access_ACCESS_FORBIDDEN
			scope.Reason = "this identity may not list schemas or migrations here"
		default:
			scope.Access = consolev1.Access_ACCESS_ERROR
			scope.Reason = "the API server did not say what this identity may do here"
		}
		out.Namespaces = append(out.Namespaces, scope)
	}
	return out, nil
}

// listable returns the namespaces the identity may list, inside the ceiling
// when one is set. An identity that may not list namespaces gets none, and
// names its namespaces itself.
func listable(ctx context.Context, c client.Client, scope Scope) []string {
	list := &corev1.NamespaceList{}
	if err := c.List(ctx, list, client.Limit(maxNamespaces)); err != nil {
		return nil
	}
	names := make([]string, 0, len(list.Items))
	for _, namespace := range list.Items {
		if len(scope.Ceiling) == 0 || slices.Contains(scope.Ceiling, namespace.Name) {
			names = append(names, namespace.Name)
		}
	}
	return names
}
