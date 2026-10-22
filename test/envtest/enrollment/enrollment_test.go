// Package enrollment_test holds enrollment to a real API server: who may
// start one, what the console records, and how a helper's report is taken
// once.
package enrollment_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/enrollment"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{RegistryCRDs: true}, &plane))
}

const caPEM = `-----BEGIN CERTIFICATE-----
MIIBkTCCATegAwIBAgIUWnJ3C7dsyDNF9oRlsVJfqqH3s20wCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----
`

// registry is a registry namespace, an administrator who may enroll, and
// what the console says of itself.
type registry struct {
	self  enrollment.Self
	admin client.Client
}

func newRegistry(t *testing.T) *registry {
	t.Helper()
	c := qt.New(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "enroll-"}}
	c.Assert(plane.Admin.Create(t.Context(), namespace), qt.IsNil)
	group := consolev1alpha1.GroupVersion.Group
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "connection-admin"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterenrollments"},
				Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{APIGroups: []string{rbacv1.GroupName}, Resources: []string{"roles", "rolebindings"}, Verbs: []string{"create"}},
		},
	}
	c.Assert(plane.Admin.Create(t.Context(), role), qt.IsNil)
	c.Assert(plane.Admin.Create(t.Context(), &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "connection-admin"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "connection-admin"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "ptah-console:prod:alex"}},
	}), qt.IsNil)
	return &registry{
		self: enrollment.Self{
			Namespace: namespace.Name, InstallationID: "prod", Accounts: []string{"alex", "sam"},
			Agents: enrollment.Console{Address: "https://agents.console.example.com", ServerName: "agents.console.example.com", CABundle: caPEM},
			Chart:  enrollment.Chart{Reference: "oci://ghcr.io/stokaro/charts/ptah-console-agent", Version: "0.2.0"},
		},
		admin: as(t, "ptah-console:prod:alex"),
	}
}

// as is a client the API server takes for user.
func as(t *testing.T, user string) client.Client {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	return built
}

func request(name string) enrollment.Request {
	return enrollment.Request{
		ClusterName: name, Namespaces: []string{"storefront", "billing"},
		Accounts:            []enrollment.Account{{ID: "alex", Roles: []string{"viewer", "approver"}}},
		ApproverClusterRole: "ptah-operator-ptah-operator-approver",
	}
}

func (r *registry) create(t *testing.T, name string, now time.Time) *enrollment.File {
	t.Helper()
	file, err := enrollment.Create(t.Context(), r.admin, plane.Admin, r.self, request(name), now)
	qt.New(t).Assert(err, qt.IsNil)
	return file
}

func (r *registry) enrollment(t *testing.T, name string) *consolev1alpha1.ConsoleClusterEnrollment {
	t.Helper()
	object := &consolev1alpha1.ConsoleClusterEnrollment{}
	qt.New(t).Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: r.self.Namespace, Name: name}, object), qt.IsNil)
	return object
}

func report(file *enrollment.File, credential string) enrollment.Completion {
	return enrollment.Completion{
		ConnectionUID: file.Identity.ConnectionUID, Bootstrap: file.Bootstrap, Fingerprint: file.Fingerprint,
		TargetServer: "https://api.prod-ap-2.example.com:6443", TargetCASHA256: "sha256:" + strings.Repeat("1", 64),
		CredentialVerifier: enrollment.Verifier(credential),
	}
}

// An administrator starts an enrollment as themselves; the console records
// the bootstrap's verifier, never the secret, and the file it hands out is
// whole.
func TestAnAdministratorStartsAnEnrollment(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := newRegistry(t)
	now := time.Now().Truncate(time.Second)

	file := r.create(t, "prod-ap-2", now)

	encoded, err := file.Marshal()
	c.Assert(err, qt.IsNil)
	parsed, err := enrollment.Parse(encoded)
	c.Assert(err, qt.IsNil)
	c.Assert(parsed.Identity.Namespaces, qt.DeepEquals, []string{"storefront", "billing"})
	stored := r.enrollment(t, "prod-ap-2")
	c.Assert(stored.Spec.ConnectionUID, qt.Equals, file.Identity.ConnectionUID)
	c.Assert(stored.Spec.ConsoleCASHA256, qt.Equals, enrollment.CASHA256(caPEM))
	c.Assert(stored.Status.BootstrapVerifier, qt.Equals, enrollment.Verifier(file.Bootstrap))
	c.Assert(stored.Status.BootstrapVerifier, qt.Not(qt.Equals), file.Bootstrap)
	c.Assert(stored.Status.Fingerprint, qt.Equals, file.Fingerprint)
	c.Assert(stored.Status.ExpiresAt.Time, qt.Equals, now.Add(enrollment.BootstrapValidity))
}

// Who may not enroll is refused by the API server, and a name a connection
// or another enrollment has, an account the console lacks, or one
// enrollment past the bound is refused by the console.
func TestEachEnrollmentRefusal(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := newRegistry(t)
	now := time.Now()
	r.create(t, "pending", now)
	c.Assert(plane.Admin.Create(t.Context(), &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: r.self.Namespace, Name: "taken"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d", Profile: consolev1alpha1.ProfileCoLocated, InstallationID: "prod",
			Namespaces: []string{"storefront"}, ConfigurationOwner: consolev1alpha1.OwnerGitOps,
		},
	}), qt.IsNil)

	_, err := enrollment.Create(t.Context(), as(t, "ptah-console:prod:sam"), plane.Admin, r.self, request("other"), now)
	c.Assert(apierrors.IsForbidden(err), qt.IsTrue, qt.Commentf("%v", err))
	_, err = enrollment.Create(t.Context(), r.admin, plane.Admin, r.self, request("pending"), now)
	c.Assert(err, qt.ErrorIs, enrollment.ErrNameTaken)
	_, err = enrollment.Create(t.Context(), r.admin, plane.Admin, r.self, request("taken"), now)
	c.Assert(err, qt.ErrorIs, enrollment.ErrNameTaken)
	unknown := request("unknown")
	unknown.Accounts = []enrollment.Account{{ID: "lee", Roles: []string{"viewer"}}}
	_, err = enrollment.Create(t.Context(), r.admin, plane.Admin, r.self, unknown, now)
	c.Assert(err, qt.ErrorIs, enrollment.ErrUnknownAccount)

	for index := range enrollment.MaxPending - 1 {
		r.create(t, fmt.Sprintf("fill-%d", index), now)
	}
	_, err = enrollment.Create(t.Context(), r.admin, plane.Admin, r.self, request("one-more"), now)
	c.Assert(err, qt.ErrorIs, enrollment.ErrTooMany)
}

// The helper's report is taken once: again as it was, never otherwise, and
// not with a wrong, expired or altered file.
func TestAReportIsTakenOnce(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := newRegistry(t)
	issued := time.Now().Truncate(time.Second)
	file := r.create(t, "prod-ap-2", issued)
	before := issued.Add(enrollment.BootstrapValidity - time.Second)

	wrong := report(file, "credential")
	wrong.Bootstrap = "not-the-bootstrap"
	_, err := enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, wrong, before)
	c.Assert(err, qt.ErrorIs, enrollment.ErrNotAccepted)
	altered := report(file, "credential")
	altered.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	_, err = enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, altered, before)
	c.Assert(err, qt.ErrorIs, enrollment.ErrNotThisFile)
	_, err = enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, report(file, "credential"), issued.Add(enrollment.BootstrapValidity))
	c.Assert(err, qt.ErrorIs, enrollment.ErrNotAccepted, qt.Commentf("an expired bootstrap was taken"))

	completed, err := enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, report(file, "credential"), before)
	c.Assert(err, qt.IsNil)
	c.Assert(completed.Status.Completion.CredentialVerifier, qt.Equals, enrollment.Verifier("credential"))
	_, err = enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, report(file, "credential"), issued.Add(time.Hour))
	c.Assert(err, qt.IsNil, qt.Commentf("the same report, sent again after the answer was lost, was refused"))
	_, err = enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, report(file, "another credential"), before)
	c.Assert(err, qt.ErrorIs, enrollment.ErrCompletedOtherwise)
	c.Assert(r.enrollment(t, "prod-ap-2").Status.Completion.CredentialVerifier, qt.Equals, enrollment.Verifier("credential"))
}

// Two helpers racing with one bootstrap: one report is taken, and the other
// is refused.
func TestOneOfTwoRacingReportsIsTaken(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := newRegistry(t)
	now := time.Now()
	file := r.create(t, "prod-ap-2", now)
	errs := make([]error, 2)
	var racing sync.WaitGroup
	for index := range errs {
		racing.Go(func() {
			_, errs[index] = enrollment.Complete(context.Background(), plane.Admin, r.self.Namespace, report(file, fmt.Sprintf("credential-%d", index)), now)
		})
	}
	racing.Wait()

	taken := slices.DeleteFunc(slices.Clone(errs), func(err error) bool { return err != nil })
	refused := slices.DeleteFunc(slices.Clone(errs), func(err error) bool { return !errors.Is(err, enrollment.ErrCompletedOtherwise) })
	c.Assert(taken, qt.HasLen, 1, qt.Commentf("%v", errs))
	c.Assert(refused, qt.HasLen, 1, qt.Commentf("%v", errs))
}

// An unused enrollment is gone once its bootstrap expires, and a completed
// one once the activation window passes.
func TestExpiredEnrollmentsAreRemoved(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := newRegistry(t)
	issued := time.Now().Truncate(time.Second)
	r.create(t, "unused", issued)
	completedFile := r.create(t, "completed", issued)
	_, err := enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, report(completedFile, "credential"), issued)
	c.Assert(err, qt.IsNil)
	names := func() []string {
		list := &consolev1alpha1.ConsoleClusterEnrollmentList{}
		c.Assert(plane.Admin.List(t.Context(), list, client.InNamespace(r.self.Namespace)), qt.IsNil)
		var out []string
		for _, item := range list.Items {
			out = append(out, item.Name)
		}
		return out
	}

	c.Assert(enrollment.Tend(t.Context(), plane.Admin, r.self.Namespace, issued.Add(enrollment.BootstrapValidity)), qt.IsNil)
	c.Assert(names(), qt.DeepEquals, []string{"completed"})
	c.Assert(enrollment.Tend(t.Context(), plane.Admin, r.self.Namespace, issued.Add(enrollment.ActivationWindow)), qt.IsNil)
	c.Assert(names(), qt.HasLen, 0)
}

// connected completes an enrollment of alex and sam and records its agent
// connected, as the helper and the agent listener would.
func (r *registry) connected(t *testing.T, name string, now time.Time) *consolev1alpha1.ConsoleClusterEnrollment {
	t.Helper()
	c := qt.New(t)
	enrolled := request(name)
	enrolled.Accounts = append(enrolled.Accounts, enrollment.Account{ID: "sam", Roles: []string{"viewer"}})
	file, err := enrollment.Create(t.Context(), r.admin, plane.Admin, r.self, enrolled, now)
	c.Assert(err, qt.IsNil)
	_, err = enrollment.Complete(t.Context(), plane.Admin, r.self.Namespace, report(file, "credential-"+name), now)
	c.Assert(err, qt.IsNil)
	pending := r.enrollment(t, name)
	meta.SetStatusCondition(&pending.Status.Conditions, metav1.Condition{
		Type: consolev1alpha1.ConditionAgentConnected, Status: metav1.ConditionTrue, Reason: "Connected", Message: "the agent opened its channel",
	})
	c.Assert(plane.Admin.Status().Update(t.Context(), pending), qt.IsNil)
	return pending
}

// entryOf is the registry entry an activation of pending creates, under
// another connection UID when one is given.
func entryOf(pending *consolev1alpha1.ConsoleClusterEnrollment, connectionUID string) *consolev1alpha1.ConsoleCluster {
	return &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: pending.Namespace, Name: pending.Spec.ClusterName},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: connectionUID, Profile: consolev1alpha1.ProfileAgent, InstallationID: pending.Spec.InstallationID,
			Namespaces: pending.Spec.Namespaces, ConfigurationOwner: consolev1alpha1.OwnerConsole,
			Agent: &consolev1alpha1.AgentTrust{
				TargetServer: pending.Status.Completion.TargetServer, TargetCASHA256: pending.Status.Completion.TargetCASHA256,
				ConsoleServerName: pending.Spec.ConsoleServerName, ConsoleCASHA256: pending.Spec.ConsoleCASHA256,
			},
		},
	}
}

// An activation that stopped after it created the entry is finished by the
// next one: the channel credential is recorded and the enrolled accounts may
// get the entry. An entry of the same name for another connection is not
// taken over.
func TestAnActivationThatStoppedPartWayResumes(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := newRegistry(t)
	now := time.Now().Truncate(time.Second)
	pending := r.connected(t, "prod-ap-2", now)
	c.Assert(r.admin.Create(t.Context(), entryOf(pending, pending.Spec.ConnectionUID)), qt.IsNil)
	other := r.connected(t, "prod-ap-3", now)
	c.Assert(r.admin.Create(t.Context(), entryOf(other, "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d")), qt.IsNil)

	entry, err := enrollment.Activate(t.Context(), r.admin, plane.Admin, r.self.Namespace, "prod-ap-2", now, 90*24*time.Hour)

	c.Assert(err, qt.IsNil)
	stored := &consolev1alpha1.ConsoleCluster{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(entry), stored), qt.IsNil)
	c.Assert(stored.Status.Channel.Credentials[0].Verifier, qt.Equals, enrollment.Verifier("credential-prod-ap-2"))
	c.Assert(as(t, "ptah-console:prod:sam").Get(t.Context(), client.ObjectKeyFromObject(entry), &consolev1alpha1.ConsoleCluster{}), qt.IsNil,
		qt.Commentf("an enrolled account may not get the entry"))
	kim := as(t, "ptah-console:prod:kim")
	c.Assert(apierrors.IsForbidden(kim.Get(t.Context(), client.ObjectKeyFromObject(entry), &consolev1alpha1.ConsoleCluster{})), qt.IsTrue,
		qt.Commentf("an account the enrollment did not name may get the entry"))
	_, err = enrollment.Activate(t.Context(), r.admin, plane.Admin, r.self.Namespace, "prod-ap-3", now, 90*24*time.Hour)
	c.Assert(err, qt.ErrorIs, enrollment.ErrNameTaken)
}
