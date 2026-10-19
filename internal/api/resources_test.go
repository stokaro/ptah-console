package api_test

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"github.com/stokaro/ptah-operator/pkg/planread"
	"github.com/stokaro/ptah-operator/pkg/planread/planreadtest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

var localPrincipal = kube.Principal{Mode: kube.ModeLocal, Context: "test", Generation: 1}

// staticFactory acts for any principal with one client, which is what a test
// of a handler needs: the identity question is the factory's own tests'.
type staticFactory struct{ client client.Client }

func (f staticFactory) For(kube.Principal) (*kube.Client, error) {
	return &kube.Client{Client: f.client}, nil
}

// signedIn returns a context carrying a session cookie for the store.
func signedIn(t *testing.T, store *session.Store) context.Context {
	t.Helper()
	token, _, err := store.Create(localPrincipal)
	qt.New(t).Assert(err, qt.IsNil)
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	return ctx
}

func schemaService(t *testing.T, reader client.Client) (consolev1connect.SchemaServiceClient, context.Context) {
	t.Helper()
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterSchemaServiceHandler(server, api.NewSchemaService(staticFactory{reader}, api.Scope{}))
	return consolev1connect.NewSchemaServiceClient(connect.NewClient(connectinprocess.New(server))), signedIn(t, store)
}

// The console answers every stored state the operator's own reader answers,
// through its API: SQL only for a plan that held every check, a reason and no
// SQL for one that did not.
func TestGetSchemaPlanAnswersTheReadersCases(t *testing.T) {
	t.Parallel()
	for _, test := range planreadtest.Cases(t) {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			schemas, ctx := schemaService(t, planreadtest.NewClient(t, test.Objects...))

			response, err := schemas.GetSchemaPlan(ctx, &consolev1.GetSchemaPlanRequest{
				Namespace: planreadtest.Namespace, Schema: test.Schema, Selection: selection(test.Selection),
			})

			c.Assert(observe(response, err), qt.DeepEquals, expectation(test.Want))
		})
	}
}

// outcome is what a caller of GetSchemaPlan sees: the code it got, whether
// SQL came back, and the SQL and plan name when it did.
type outcome struct {
	Code       connect.Code
	Access     consolev1.SqlAccess
	PlanName   string
	Statements []string
}

func observe(response *consolev1.GetSchemaPlanResponse, err error) outcome {
	if err != nil {
		return outcome{Code: connect.CodeOf(err)}
	}
	plan := response.GetPlan()
	seen := outcome{Access: plan.GetSqlAccess(), Statements: sqlOf(plan)}
	if seen.Access == consolev1.SqlAccess_SQL_ACCESS_AVAILABLE {
		seen.PlanName = plan.GetName()
	}
	return seen
}

// expectation is what the API owes for each answer the reader gives.
func expectation(want planreadtest.Want) outcome {
	switch {
	case want.Err == nil:
		return outcome{Access: consolev1.SqlAccess_SQL_ACCESS_AVAILABLE, PlanName: want.PlanName, Statements: want.Statements}
	case errors.Is(want.Err, planread.ErrIntegrity):
		return outcome{Access: consolev1.SqlAccess_SQL_ACCESS_INTEGRITY_FAILED, Statements: make([]string, 0)}
	case errors.Is(want.Err, planread.ErrUnsupported):
		return outcome{Access: consolev1.SqlAccess_SQL_ACCESS_UNSUPPORTED, Statements: make([]string, 0)}
	default:
		return outcome{Code: connect.CodeNotFound}
	}
}

func selection(s planread.Selection) consolev1.PlanSelection {
	if s == planread.Applied {
		return consolev1.PlanSelection_PLAN_SELECTION_APPLIED
	}
	return consolev1.PlanSelection_PLAN_SELECTION_CURRENT
}

func sqlOf(plan *consolev1.SchemaPlan) []string {
	out := make([]string, 0, len(plan.GetStatements()))
	for _, statement := range plan.GetStatements() {
		out = append(out, statement.GetSql())
	}
	return out
}

// An identity that may read the plan and not its chunks is told so, with the
// manifest and no SQL.
func TestAPlanWhoseChunksAreNotGrantedComesBackWithoutSQL(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	lab := planreadtest.NewLab(t, "storefront")
	stored, _ := lab.Publish(t, []string{"CREATE TABLE customers (id BIGINT PRIMARY KEY)"}, "v1")
	lab.SetCurrent(t, stored)
	schemas, ctx := schemaService(t, chunkForbidding{Client: lab.Client()})

	response, err := schemas.GetSchemaPlan(ctx, &consolev1.GetSchemaPlanRequest{
		Namespace: planreadtest.Namespace, Schema: "storefront", Selection: consolev1.PlanSelection_PLAN_SELECTION_CURRENT,
	})

	c.Assert(err, qt.IsNil)
	plan := response.GetPlan()
	c.Assert(plan.GetSqlAccess(), qt.Equals, consolev1.SqlAccess_SQL_ACCESS_NOT_GRANTED)
	c.Assert(plan.GetStatements(), qt.HasLen, 0)
	c.Assert(plan.GetFingerprint(), qt.Equals, stored.Spec.Fingerprint)
	c.Assert(plan.GetSqlProblem(), qt.Contains, "ptahschemaplanchunks")
}

// A namespace outside the console's ceiling is refused before anything is
// read, whatever the identity may read there.
func TestTheCeilingRefusesAnotherNamespace(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	reader := &counting{Client: planreadtest.NewClient(t)}
	consolev1connect.RegisterSchemaServiceHandler(server, api.NewSchemaService(staticFactory{reader}, api.Scope{Ceiling: []string{"application"}}))
	schemas := consolev1connect.NewSchemaServiceClient(connect.NewClient(connectinprocess.New(server)))

	_, err := schemas.ListSchemas(signedIn(t, store), &consolev1.ListSchemasRequest{Namespaces: []string{"application", "billing"}})

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied)
	c.Assert(reader.reads, qt.Equals, 0)
}

type chunkForbidding struct{ client.Client }

func (r chunkForbidding) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if _, ok := object.(*operatorv1alpha1.PtahSchemaPlanChunk); ok {
		resource := operatorv1alpha1.GroupVersion.WithResource("ptahschemaplanchunks").GroupResource()
		return apierrors.NewForbidden(resource, key.Name, errors.New("no rule grants it"))
	}
	return r.Client.Get(ctx, key, object, options...)
}

type counting struct {
	client.Client
	reads int
}

func (r *counting) List(ctx context.Context, list client.ObjectList, options ...client.ListOption) error {
	r.reads++
	return r.Client.List(ctx, list, options...)
}
