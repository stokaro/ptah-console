package api_test

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// authoring is the service over a fake API server that records each create
// and whether it was a dry run.
type authoring struct {
	client  consolev1connect.AuthoringServiceClient
	ctx     context.Context
	stored  client.Client
	audit   *bytes.Buffer
	mu      *sync.Mutex
	dryRuns *[]bool
}

func authoringService(t *testing.T, scope api.Scope, refuse error, objects ...client.Object) authoring {
	t.Helper()
	scheme, err := kube.Scheme()
	qt.New(t).Assert(err, qt.IsNil)
	var mu sync.Mutex
	var dryRuns []bool
	built := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, object client.Object, options ...client.CreateOption) error {
			created := &client.CreateOptions{}
			created.ApplyOptions(options)
			mu.Lock()
			dryRuns = append(dryRuns, slices.Equal(created.DryRun, []string{"All"}))
			mu.Unlock()
			if refuse != nil {
				return refuse
			}
			return c.Create(ctx, object, options...)
		},
	}).Build()
	log := &bytes.Buffer{}
	store := session.NewStore(session.Options{})
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterAuthoringServiceHandler(server, api.NewAuthoringService(staticFactory{built}, scope, audit.New(log)))
	return authoring{
		client: consolev1connect.NewAuthoringServiceClient(connect.NewClient(connectinprocess.New(server))),
		ctx:    signedIn(t, store), stored: built, audit: log, mu: &mu, dryRuns: &dryRuns,
	}
}

func (a authoring) sent() []bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(*a.dryRuns)
}

func draft(source *consolev1.SourceDraft) *consolev1.ResourceDraft {
	return &consolev1.ResourceDraft{
		Namespace: namespace, Name: "storefront",
		Target: &consolev1.TargetDraft{
			Engine: "PostgreSQL", CoordinationKey: "production/storefront",
			UrlSecretName: "storefront-database", UrlSecretKey: "url",
		},
		Source: source, ApplyPolicy: "OnApproval",
	}
}

var ociSource = &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_Oci{Oci: &consolev1.OciSourceDraft{
	Ref: "oci://ghcr.io/example/storefront:1.0.0", VerificationPolicyName: "ptah-verification-policy", VerificationPolicyKey: "policy.yaml",
}}}

// The object is built from the fields, each source kind in its own spelling
// of the operator's API, and nothing else is set.
func TestAuthoringBuildsTheObjectFromTheFields(t *testing.T) {
	t.Parallel()
	target := operatorv1alpha1.DatabaseTargetSpec{
		Engine: "PostgreSQL", CoordinationKey: "production/storefront",
		URLFrom: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "storefront-database"}, Key: "url"},
	}
	tests := []struct {
		name   string
		source *consolev1.SourceDraft
		want   operatorv1alpha1.SourceSpec
	}{
		{name: "an OCI artifact", source: ociSource, want: operatorv1alpha1.SourceSpec{
			OCIRef: "oci://ghcr.io/example/storefront:1.0.0",
			VerificationPolicyFrom: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "ptah-verification-policy"}, Key: "policy.yaml",
			},
		}},
		{name: "a ConfigMap", source: &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_ConfigMap{ConfigMap: &consolev1.ConfigMapSourceDraft{
			Name: "storefront-schema", Key: "schema.sql", Path: "schema.sql",
		}}}, want: operatorv1alpha1.SourceSpec{ConfigMap: &operatorv1alpha1.ConfigMapSourceSpec{
			Name: "storefront-schema", Items: []operatorv1alpha1.ConfigMapSourceItem{{Key: "schema.sql", Path: "schema.sql"}},
		}}},
		{name: "a Git branch", source: &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_Git{Git: &consolev1.GitSourceDraft{
			Url: "https://git.example.com/shop/schema.git", Branch: "main", Path: "schema.sql", CredentialsSecret: "shop-git",
		}}}, want: operatorv1alpha1.SourceSpec{Git: &operatorv1alpha1.GitSourceSpec{
			URL: "https://git.example.com/shop/schema.git", Ref: operatorv1alpha1.GitRef{Branch: "main"}, Path: "schema.sql",
			AuthFrom: &operatorv1alpha1.GitAuthSource{Name: "shop-git"},
		}}},
		{name: "a Git tag over SSH, with its known hosts", source: &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_Git{Git: &consolev1.GitSourceDraft{
			Url: "ssh://git@git.example.com/shop/schema.git", Tag: "v1.0.0", Path: "schema.sql", CredentialsSecret: "shop-git",
			KnownHostsConfigMap: "git-known-hosts", KnownHostsKey: "known_hosts",
		}}}, want: operatorv1alpha1.SourceSpec{Git: &operatorv1alpha1.GitSourceSpec{
			URL: "ssh://git@git.example.com/shop/schema.git", Ref: operatorv1alpha1.GitRef{Tag: "v1.0.0"}, Path: "schema.sql",
			AuthFrom: &operatorv1alpha1.GitAuthSource{Name: "shop-git"},
			Transport: operatorv1alpha1.GitTransportSpec{KnownHostsFrom: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "git-known-hosts"}, Key: "known_hosts",
			}},
		}}},
		{name: "a Git commit behind a private CA", source: &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_Git{Git: &consolev1.GitSourceDraft{
			Url: "https://git.example.com/shop/schema.git", Commit: "0123456789abcdef0123456789abcdef01234567", Path: "schema.sql",
			CaConfigMap: "git-ca", CaKey: "ca.pem",
		}}}, want: operatorv1alpha1.SourceSpec{Git: &operatorv1alpha1.GitSourceSpec{
			URL: "https://git.example.com/shop/schema.git", Ref: operatorv1alpha1.GitRef{Commit: "0123456789abcdef0123456789abcdef01234567"}, Path: "schema.sql",
			Transport: operatorv1alpha1.GitTransportSpec{CAFrom: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "git-ca"}, Key: "ca.pem",
			}},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := authoringService(t, api.Scope{}, nil)

			schemaAnswer, err := service.client.CreateSchema(service.ctx, &consolev1.CreateSchemaRequest{Draft: draft(test.source)})
			c.Assert(err, qt.IsNil)
			migrationDraft := draft(test.source)
			migrationDraft.Name = "billing"
			migrationAnswer, err := service.client.CreateMigration(service.ctx, &consolev1.CreateMigrationRequest{Draft: migrationDraft})
			c.Assert(err, qt.IsNil)

			c.Assert(schemaAnswer.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
			c.Assert(migrationAnswer.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
			schema := &operatorv1alpha1.PtahSchema{}
			c.Assert(service.stored.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "storefront"}, schema), qt.IsNil)
			c.Assert(schema.Spec, qt.DeepEquals, operatorv1alpha1.PtahSchemaSpec{
				Target: target, Desired: test.want, Policy: operatorv1alpha1.ReconciliationPolicy{Apply: "OnApproval"},
			})
			migration := &operatorv1alpha1.PtahMigration{}
			c.Assert(service.stored.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "billing"}, migration), qt.IsNil)
			c.Assert(migration.Spec, qt.DeepEquals, operatorv1alpha1.PtahMigrationSpec{
				Target: target, Artifact: test.want, Policy: operatorv1alpha1.MigrationPolicy{Apply: "OnApproval"},
			})
		})
	}
}

// A migration directory in a ConfigMap is every file the draft lists, each
// key as the file it becomes.
func TestAMigrationTakesEveryFileOfItsConfigMap(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := authoringService(t, api.Scope{}, nil)
	sent := draft(&consolev1.SourceDraft{Kind: &consolev1.SourceDraft_ConfigMap{ConfigMap: &consolev1.ConfigMapSourceDraft{
		Name: "billing-migrations",
		Files: []*consolev1.ConfigMapFile{
			{Key: "0000000001-up", Path: "0000000001_create_accounts.up.sql"},
			{Key: "0000000001-down", Path: "0000000001_create_accounts.down.sql"},
			{Key: "0000000002-up", Path: "0000000002_add_invoices.up.sql"},
		},
	}}})
	sent.Name = "billing"

	answer, err := service.client.CreateMigration(service.ctx, &consolev1.CreateMigrationRequest{Draft: sent})

	c.Assert(err, qt.IsNil)
	c.Assert(answer.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
	migration := &operatorv1alpha1.PtahMigration{}
	c.Assert(service.stored.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "billing"}, migration), qt.IsNil)
	c.Assert(migration.Spec.Artifact.ConfigMap, qt.DeepEquals, &operatorv1alpha1.ConfigMapSourceSpec{
		Name: "billing-migrations",
		Items: []operatorv1alpha1.ConfigMapSourceItem{
			{Key: "0000000001-up", Path: "0000000001_create_accounts.up.sql"},
			{Key: "0000000001-down", Path: "0000000001_create_accounts.down.sql"},
			{Key: "0000000002-up", Path: "0000000002_add_invoices.up.sql"},
		},
	})
}

// A check sends a dry run: the API server's answer comes back with the
// object as YAML, nothing is stored, and nothing is audited, since nothing
// changed.
func TestACheckStoresNothing(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := authoringService(t, api.Scope{}, nil)

	answer, err := service.client.CreateSchema(service.ctx, &consolev1.CreateSchemaRequest{Draft: draft(ociSource), DryRun: true})

	c.Assert(err, qt.IsNil)
	result := answer.GetResult()
	c.Assert(result.GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED)
	c.Assert(result.GetDryRun(), qt.IsTrue)
	c.Assert(result.GetYaml(), qt.Contains, "kind: PtahSchema\n")
	c.Assert(result.GetYaml(), qt.Contains, "ociRef: oci://ghcr.io/example/storefront:1.0.0")
	c.Assert(result.GetYaml(), qt.Not(qt.Contains), "managedFields")
	c.Assert(service.sent(), qt.DeepEquals, []bool{true})
	err = service.stored.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "storefront"}, &operatorv1alpha1.PtahSchema{})
	c.Assert(apierrors.IsNotFound(err), qt.IsTrue, qt.Commentf("%v", err))
	c.Assert(service.audit.String(), qt.Equals, "")
}

// A create is audited with how it ended.
func TestACreateIsAudited(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	service := authoringService(t, api.Scope{}, nil)

	answer, err := service.client.CreateSchema(service.ctx, &consolev1.CreateSchemaRequest{Draft: draft(ociSource)})

	c.Assert(err, qt.IsNil)
	c.Assert(answer.GetResult().GetRef().GetName(), qt.Equals, "storefront")
	c.Assert(service.sent(), qt.DeepEquals, []bool{false})
	c.Assert(service.audit.String(), qt.Contains, `"action":"create-schema"`)
	c.Assert(service.audit.String(), qt.Contains, `"outcome":"admitted"`)
	c.Assert(service.audit.String(), qt.Contains, `"kind":"PtahSchema"`)
}

// The API server's refusal comes back in its own words, sorted as the
// decisions are, and one that already exists says so.
func TestAuthoringPassesTheAPIServersAnswerOn(t *testing.T) {
	t.Parallel()
	resource := schema.GroupResource{Group: "operator.ptah.run", Resource: "ptahschemas"}
	tests := []struct {
		name    string
		refuse  error
		outcome consolev1.DecisionOutcome
		says    string
	}{
		{name: "RBAC", refuse: apierrors.NewForbidden(resource, "storefront", errSays(`User "alex" cannot create resource "ptahschemas"`)),
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN, says: `cannot create resource "ptahschemas"`},
		{name: "the CRD", refuse: apierrors.NewInvalid(schema.GroupKind{Group: "operator.ptah.run", Kind: "PtahSchema"}, "storefront", nil),
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED, says: `PtahSchema.operator.ptah.run "storefront" is invalid`},
		{name: "an existing name", refuse: apierrors.NewAlreadyExists(resource, "storefront"),
			outcome: consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED,
			says:    "PtahSchema storefront already exists in namespace " + namespace + ". Nothing was created."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := authoringService(t, api.Scope{}, test.refuse)

			answer, err := service.client.CreateSchema(service.ctx, &consolev1.CreateSchemaRequest{Draft: draft(ociSource)})

			c.Assert(err, qt.IsNil)
			c.Assert(answer.GetResult().GetOutcome(), qt.Equals, test.outcome)
			c.Assert(answer.GetResult().GetMessage(), qt.Contains, test.says)
			c.Assert(answer.GetResult().GetYaml(), qt.Equals, "")
		})
	}
}

type errSays string

func (e errSays) Error() string { return string(e) }

// A draft the console cannot spell as an object, or one outside the
// ceiling, is refused before anything is sent.
func TestAuthoringRefusesADraftBeforeSendingIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		scope  api.Scope
		change func(*consolev1.ResourceDraft)
		code   connect.Code
		says   string
	}{
		{name: "no name", change: func(d *consolev1.ResourceDraft) { d.Name = "" }, code: connect.CodeInvalidArgument, says: "name is required"},
		{name: "an engine the operator does not support", change: func(d *consolev1.ResourceDraft) { d.Target.Engine = "Oracle" },
			code: connect.CodeInvalidArgument, says: `engine "Oracle" is not one the operator supports`},
		{name: "no source", change: func(d *consolev1.ResourceDraft) { d.Source = nil },
			code: connect.CodeInvalidArgument, says: "a source is required: an OCI artifact, a ConfigMap or a Git repository"},
		{name: "one ConfigMap file by key and more as files", change: func(d *consolev1.ResourceDraft) {
			d.Source = &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_ConfigMap{ConfigMap: &consolev1.ConfigMapSourceDraft{
				Name: "storefront-schema", Key: "schema.sql", Path: "schema.sql",
				Files: []*consolev1.ConfigMapFile{{Key: "extra.sql", Path: "extra.sql"}},
			}}}
		}, code: connect.CodeInvalidArgument, says: "a ConfigMap source names one file by key and path, or several as files, not both"},
		{name: "an apply policy that is none", change: func(d *consolev1.ResourceDraft) { d.ApplyPolicy = "Sometimes" },
			code: connect.CodeInvalidArgument, says: `apply policy "Sometimes" is not Never, OnApproval or Always`},
		{name: "a namespace outside the ceiling", scope: api.Scope{Ceiling: []string{"billing"}}, change: func(*consolev1.ResourceDraft) {},
			code: connect.CodePermissionDenied, says: `namespace "` + namespace + `" is outside what this console serves`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			service := authoringService(t, test.scope, nil)
			sent := draft(ociSource)
			test.change(sent)

			_, err := service.client.CreateSchema(service.ctx, &consolev1.CreateSchemaRequest{Draft: sent})

			c.Assert(connect.CodeOf(err), qt.Equals, test.code)
			c.Assert(err, qt.ErrorMatches, `.*`+regexp.QuoteMeta(test.says)+`.*`)
			c.Assert(service.sent(), qt.HasLen, 0)
		})
	}
}
