// Package authoring_test creates schemas and migrations through the console
// against a real API server with the operator's CRDs, as a user RBAC grants
// what each row needs: the CRD's schema and CEL rules judge a draft, a dry
// run stores nothing, and a create runs as the user.
package authoring_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{OperatorCRDs: true}, &plane))
}

type factory struct{ client *kube.Client }

func (f factory) For(kube.Principal) (*kube.Client, error) { return f.client, nil }

// arrange makes a namespace and lets user do the verbs on schemas there.
func arrange(t *testing.T, user string, verbs ...string) string {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	namespace := user + "-authoring"
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
	if len(verbs) == 0 {
		return namespace
	}
	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "author"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas", "ptahmigrations"}, Verbs: verbs,
		}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "author"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "author"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: user}},
	}), qt.IsNil)
	return namespace
}

// serviceAs serves the authoring service as user, in process, and returns
// it with the audit log it writes.
func serviceAs(t *testing.T, user string) (consolev1connect.AuthoringServiceClient, context.Context, *bytes.Buffer) {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(kube.Principal{Mode: kube.ModeLocal, Context: "envtest", Generation: 1})
	c.Assert(err, qt.IsNil)
	log := &bytes.Buffer{}
	server := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterAuthoringServiceHandler(server, api.NewAuthoringService(factory{built}, api.Scope{}, audit.New(log)))
	ctx, info := connect.NewClientContext(t.Context())
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	return consolev1connect.NewAuthoringServiceClient(connect.NewClient(connectinprocess.New(server))), ctx, log
}

// draft is the operator's smallest schema, as its reference example writes
// it, in namespace.
func draft(namespace string) *consolev1.ResourceDraft {
	return &consolev1.ResourceDraft{
		Namespace: namespace, Name: "application",
		Target: &consolev1.TargetDraft{
			Engine: "PostgreSQL", CoordinationKey: "production/application-primary",
			UrlSecretName: "application-database", UrlSecretKey: "url",
		},
		Source: &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_Oci{Oci: &consolev1.OciSourceDraft{
			Ref:                    "oci://ghcr.io/example/application-schema:1.4.0",
			VerificationPolicyName: "ptah-verification-policy", VerificationPolicyKey: "policy.yaml",
		}}},
	}
}

// A check is judged by the CRD and stores nothing; it reads back with the
// defaults the API server filled in. The create that follows stores the
// object as the user, under the console's field manager.
func TestACheckThenACreateAsTheUser(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "author", "create")
	service, ctx, log := serviceAs(t, "author")

	checked, err := service.CreateSchema(ctx, &consolev1.CreateSchemaRequest{Draft: draft(namespace), DryRun: true})

	c.Assert(err, qt.IsNil)
	c.Assert(checked.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", checked.GetResult().GetMessage()))
	c.Assert(checked.GetResult().GetYaml(), qt.Contains, "apply: OnApproval", qt.Commentf("the CRD's default reads back"))
	err = plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "application"}, &operatorv1alpha1.PtahSchema{})
	c.Assert(apierrors.IsNotFound(err), qt.IsTrue, qt.Commentf("a dry run stored %v", err))
	c.Assert(log.String(), qt.Equals, "")

	created, err := service.CreateSchema(ctx, &consolev1.CreateSchemaRequest{Draft: draft(namespace)})

	c.Assert(err, qt.IsNil)
	c.Assert(created.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", created.GetResult().GetMessage()))
	stored := &operatorv1alpha1.PtahSchema{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: "application"}, stored), qt.IsNil)
	c.Assert(created.GetResult().GetRef().GetUid(), qt.Equals, string(stored.UID))
	c.Assert(stored.ManagedFields, qt.HasLen, 1)
	c.Assert(stored.ManagedFields[0].Manager, qt.Equals, "ptah-console")
	c.Assert(log.String(), qt.Contains, `"action":"create-schema"`)

	again, err := service.CreateSchema(ctx, &consolev1.CreateSchemaRequest{Draft: draft(namespace)})
	c.Assert(err, qt.IsNil)
	c.Assert(again.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(again.GetResult().GetMessage(), qt.Equals, "PtahSchema application already exists in namespace "+namespace+". Nothing was created.")
}

// A draft the CRD refuses comes back with the CRD's own words, by field, and
// one the identity may not create is forbidden by RBAC, not refused.
func TestTheAPIServerJudgesTheDraft(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "careful", "create")
	unbound := arrange(t, "outsider")
	service, ctx, _ := serviceAs(t, "careful")
	both := draft(namespace)
	both.Target.Realm = "shared-primary"
	noKey := draft(namespace)
	noKey.Target.UrlSecretKey = ""

	refusedBoth, err := service.CreateSchema(ctx, &consolev1.CreateSchemaRequest{Draft: both, DryRun: true})
	c.Assert(err, qt.IsNil)
	refusedKey, err := service.CreateMigration(ctx, &consolev1.CreateMigrationRequest{Draft: noKey, DryRun: true})
	c.Assert(err, qt.IsNil)
	forbidden, err := service.CreateSchema(ctx, &consolev1.CreateSchemaRequest{Draft: draft(unbound), DryRun: true})
	c.Assert(err, qt.IsNil)

	c.Assert(refusedBoth.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(refusedBoth.GetResult().GetMessage(), qt.Contains, "spec.target: Invalid value")
	c.Assert(refusedBoth.GetResult().GetMessage(), qt.Contains, "set exactly one of coordinationKey and realmRef")
	c.Assert(refusedKey.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(refusedKey.GetResult().GetMessage(), qt.Contains, "target.urlFrom must name a required Secret key")
	c.Assert(forbidden.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_FORBIDDEN)
	c.Assert(forbidden.GetResult().GetMessage(), qt.Contains, `cannot create resource "ptahschemas"`)
}

// The CRD, not the console, says how many files a kind takes from a
// ConfigMap and whom a Git fetch must trust: a migration directory takes
// several files and a schema exactly one, and an ssh:// repository is
// refused until it names its known hosts.
func TestTheCRDJudgesFilesAndTrust(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	namespace := arrange(t, "sources", "create")
	service, ctx, _ := serviceAs(t, "sources")
	files := func() *consolev1.SourceDraft {
		return &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_ConfigMap{ConfigMap: &consolev1.ConfigMapSourceDraft{
			Name: "billing-migrations",
			Files: []*consolev1.ConfigMapFile{
				{Key: "0000000001-up", Path: "0000000001_create_accounts.up.sql"},
				{Key: "0000000002-up", Path: "0000000002_add_invoices.up.sql"},
			},
		}}}
	}
	overSSH := func(knownHosts string) *consolev1.SourceDraft {
		return &consolev1.SourceDraft{Kind: &consolev1.SourceDraft_Git{Git: &consolev1.GitSourceDraft{
			Url: "ssh://git@git.example.com/shop/migrations.git", Branch: "main", Path: "migrations",
			KnownHostsConfigMap: knownHosts, KnownHostsKey: "known_hosts",
		}}}
	}
	migration := draft(namespace)
	migration.Source = files()
	schema := draft(namespace)
	schema.Source = files()
	untrusted := draft(namespace)
	untrusted.Source = overSSH("")
	untrusted.Source.GetGit().KnownHostsKey = ""
	trusted := draft(namespace)
	trusted.Source = overSSH("git-known-hosts")

	directory, err := service.CreateMigration(ctx, &consolev1.CreateMigrationRequest{Draft: migration, DryRun: true})
	c.Assert(err, qt.IsNil)
	twoForASchema, err := service.CreateSchema(ctx, &consolev1.CreateSchemaRequest{Draft: schema, DryRun: true})
	c.Assert(err, qt.IsNil)
	noKnownHosts, err := service.CreateMigration(ctx, &consolev1.CreateMigrationRequest{Draft: untrusted, DryRun: true})
	c.Assert(err, qt.IsNil)
	knownHosts, err := service.CreateMigration(ctx, &consolev1.CreateMigrationRequest{Draft: trusted, DryRun: true})
	c.Assert(err, qt.IsNil)

	c.Assert(directory.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", directory.GetResult().GetMessage()))
	c.Assert(directory.GetResult().GetYaml(), qt.Contains, "path: 0000000002_add_invoices.up.sql")
	c.Assert(twoForASchema.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(twoForASchema.GetResult().GetMessage(), qt.Contains, "desired.configMap selects exactly one file for a schema")
	c.Assert(noKnownHosts.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_REFUSED)
	c.Assert(noKnownHosts.GetResult().GetMessage(), qt.Contains, "an ssh:// repository needs transport.knownHostsFrom")
	c.Assert(knownHosts.GetResult().GetOutcome(), qt.Equals, consolev1.DecisionOutcome_DECISION_OUTCOME_ADMITTED, qt.Commentf("%s", knownHosts.GetResult().GetMessage()))
	c.Assert(knownHosts.GetResult().GetYaml(), qt.Contains, "knownHostsFrom:")
}
