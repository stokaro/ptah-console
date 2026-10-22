package hosted_test

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/enrollment"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/hosted"
)

// grantEnrollments gives the console its enrollment rights in the registry
// namespace, and alex an administrator's, as the chart does.
func grantEnrollments(t *testing.T, namespace string) {
	t.Helper()
	c := qt.New(t)
	group := consolev1alpha1.GroupVersion.Group
	for _, object := range []client.Object{
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "enrollment-housekeeping"},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{group}, Resources: []string{"consoleclusterenrollments"}, Verbs: []string{"get", "list", "watch", "delete"}},
				{APIGroups: []string{group}, Resources: []string{"consoleclusterenrollments/status"}, Verbs: []string{"update", "patch"}},
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "enrollment-housekeeping"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "enrollment-housekeeping"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "connection-admin"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterenrollments"},
				Verbs: []string{"get", "list", "create", "delete"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "connection-admin"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "connection-admin"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: alexUsername}},
		},
	} {
		c.Assert(plane.Admin.Create(t.Context(), object), qt.IsNil)
	}
}

// startConsole runs a hosted console for the registry with an agent
// listener, and returns the agents' address.
func (r *routing) startConsole(t *testing.T, certificates *pki) string {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	alexHash, err := accounts.Hash([]byte("alex-secret"), accounts.MinCost)
	c.Assert(err, qt.IsNil)
	samHash, err := accounts.Hash([]byte("sam-secret"), accounts.MinCost)
	c.Assert(err, qt.IsNil)
	configPath, passwordsPath := filepath.Join(dir, "configuration.yaml"), filepath.Join(dir, "passwords")
	c.Assert(os.WriteFile(configPath, []byte(`apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: envtest
publicURL: https://console.envtest
namespaces: [storefront]
accounts:
  - id: alex
    login: alex@example.com
  - id: sam
    login: sam@example.com
registry:
  namespace: `+r.namespace+`
  thisCluster: this-cluster
  connectionUID: `+routeThis+`
`+registryAgents(certificates)), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(passwordsPath, []byte("alex:"+string(alexHash)+"\nsam:"+string(samHash)+"\n"), 0o600), qt.IsNil)
	browser, agents := make(chan net.Addr, 1), make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- hosted.Run(ctx, hosted.Options{
			ConfigPath: configPath, PasswordsPath: passwordsPath, Listen: "127.0.0.1:0",
			TLSTerminatedUpstream: true, Kube: consoleConfig(t),
			Assets:      fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
			Listening:   func(addr net.Addr) { browser <- addr },
			AgentListen: "127.0.0.1:0", AgentTLSCertFile: certificates.certFile, AgentTLSKeyFile: certificates.keyFile,
			AgentListening: func(addr net.Addr) { agents <- addr },
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	r.browser = (<-browser).String()
	return (<-agents).String()
}

// An administrator starts an enrollment through the console and gets the
// file, which completes on the agent listener with its bootstrap and with
// nothing else; an account that may not enroll is refused by the API server.
func TestAnAdministratorEnrollsThroughTheConsole(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	grantEnrollments(t, r.namespace)
	certificates := newPKI(t)
	agents := r.startConsole(t, certificates)
	connections := consolev1connect.NewConnectionServiceClient(r.transport())
	request := &consolev1.CreateEnrollmentRequest{
		ClusterName: "prod-ap-9", Namespaces: []string{"storefront"},
		Accounts:            []*consolev1.EnrollmentAccount{{Id: "alex", Roles: []string{"viewer", "approver"}}},
		ApproverClusterRole: "ptah-operator-ptah-operator-approver",
	}

	_, err := connections.CreateEnrollment(to(t.Context(), r.signIn(t, "sam@example.com", "sam-secret"), ""), request)
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied, qt.Commentf("%v", err))
	created, err := connections.CreateEnrollment(to(t.Context(), r.signIn(t, "alex@example.com", "alex-secret"), ""), request)
	c.Assert(err, qt.IsNil)
	file, err := enrollment.Parse(created.GetEnrollmentFile())
	c.Assert(err, qt.IsNil)
	c.Assert(file.Fingerprint, qt.Equals, created.GetFingerprint())
	c.Assert(file.Console.CABundle, qt.Equals, certificates.caPEM)

	listener := agentv1connect.NewEnrollmentServiceClient(connect.NewClient(connecthttp.NewTransport(&http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: certificates.roots, ServerName: agentsName, MinVersion: tls.VersionTLS13}, ForceAttemptHTTP2: true,
	}}, "https://"+agents)))
	report := &agentv1.CompleteEnrollmentRequest{
		ConnectionUid: file.Identity.ConnectionUID, Bootstrap: "not-the-bootstrap", Fingerprint: file.Fingerprint,
		TargetServer: "https://api.prod-ap-9.example.com:6443", TargetCaSha256: enrollment.CASHA256("target CA"),
		CredentialVerifier: enrollment.Verifier("credential"),
	}
	_, err = listener.CompleteEnrollment(t.Context(), report)
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeUnauthenticated)
	report.Bootstrap = file.Bootstrap
	completed, err := listener.CompleteEnrollment(t.Context(), report)
	c.Assert(err, qt.IsNil)
	c.Assert(completed.GetClusterName(), qt.Equals, "prod-ap-9")
	stored := &consolev1alpha1.ConsoleClusterEnrollment{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: r.namespace, Name: "prod-ap-9"}, stored), qt.IsNil)
	c.Assert(stored.Status.Completion.TargetServer, qt.Equals, "https://api.prod-ap-9.example.com:6443")
}

// The helper on the target: it completes the enrollment the console issued
// with the credential it wrote into the agent's Secret, which never reaches
// the console, writes the agent's values, and a second run resumes with the
// same credential. A run the administrator declines changes nothing.
func TestTheHelperCompletesAndResumes(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	grantEnrollments(t, r.namespace)
	certificates := newPKI(t)
	agents := r.startConsole(t, certificates)
	connections := consolev1connect.NewConnectionServiceClient(r.transport())
	created, err := connections.CreateEnrollment(to(t.Context(), r.signIn(t, "alex@example.com", "alex-secret"), ""), &consolev1.CreateEnrollmentRequest{
		ClusterName: "prod-ap-7", Namespaces: []string{"storefront"},
		Accounts:            []*consolev1.EnrollmentAccount{{Id: "alex", Roles: []string{"viewer"}}},
		ApproverClusterRole: "ptah-operator-ptah-operator-approver",
	})
	c.Assert(err, qt.IsNil)
	file, err := enrollment.Parse(created.GetEnrollmentFile())
	c.Assert(err, qt.IsNil)
	// The file names the address the agents' Service would have; here the
	// listener is on a local port under the same certificate name.
	file.Console.Address = "https://" + agents
	agentNamespace := "agent-" + file.Identity.ConnectionUID[:8]
	helper := func(confirm bool) *enrollment.Helper {
		return &enrollment.Helper{
			File: file, Kube: plane.Env.Config, ContextName: "envtest-admin",
			Namespace: agentNamespace, Release: "ptah-console-agent", ValuesPath: filepath.Join(t.TempDir(), "values.yaml"),
			Confirm: func(string) (bool, error) { return confirm, nil }, Out: io.Discard,
		}
	}

	c.Assert(helper(false).Enroll(t.Context()), qt.ErrorIs, enrollment.ErrDeclined)
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Name: agentNamespace}, &corev1.Namespace{}), qt.Satisfies, apierrors.IsNotFound)

	first := helper(true)
	c.Assert(first.Enroll(t.Context()), qt.IsNil)
	secret := &corev1.Secret{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: agentNamespace, Name: "ptah-console-agent-credential"}, secret), qt.IsNil)
	credential := string(secret.Data["credential"])
	stored := &consolev1alpha1.ConsoleClusterEnrollment{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: r.namespace, Name: "prod-ap-7"}, stored), qt.IsNil)
	c.Assert(stored.Status.Completion.CredentialVerifier, qt.Equals, enrollment.Verifier(credential))
	c.Assert(stored.Status.Completion.TargetServer, qt.Equals, strings.TrimSuffix(plane.Env.Config.Host, "/"))
	c.Assert(stored.Status.Completion.TargetCASHA256, qt.Equals, enrollment.CASHA256(string(plane.Env.Config.CAData)))
	values, err := os.ReadFile(first.ValuesPath)
	c.Assert(err, qt.IsNil)
	c.Assert(string(values), qt.Contains, "uid: "+file.Identity.ConnectionUID)
	c.Assert(string(values), qt.Not(qt.Contains), credential)

	c.Assert(helper(true).Enroll(t.Context()), qt.IsNil, qt.Commentf("a second run of the same enrollment did not resume"))
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(secret), secret), qt.IsNil)
	c.Assert(string(secret.Data["credential"]), qt.Equals, credential)

	// Unenroll removes the Secret enroll made, and only for its connection.
	err = enrollment.Unenroll(t.Context(), plane.Admin, agentNamespace, "ptah-console-agent", "00000000-0000-4000-8000-000000000000")
	c.Assert(err, qt.ErrorIs, enrollment.ErrNotThisConnection)
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(secret), secret), qt.IsNil)
	c.Assert(enrollment.Unenroll(t.Context(), plane.Admin, agentNamespace, "ptah-console-agent", file.Identity.ConnectionUID), qt.IsNil)
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(secret), secret), qt.Satisfies, apierrors.IsNotFound)
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Name: agentNamespace}, &corev1.Namespace{}), qt.IsNil, qt.Commentf("unenroll removed the namespace"))
}

// The whole enrollment: the console issues it, the helper completes it, the
// agent connects and is sent nothing, an administrator activates it once it
// has, and the agent then serves the new connection.
func TestAnEnrolledClusterIsActivatedAndServed(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	grantEnrollments(t, r.namespace)
	certificates := newPKI(t)
	agents := r.startConsole(t, certificates)
	alex := r.signIn(t, "alex@example.com", "alex-secret")
	connections := consolev1connect.NewConnectionServiceClient(r.transport())
	created, err := connections.CreateEnrollment(to(t.Context(), alex, ""), &consolev1.CreateEnrollmentRequest{
		ClusterName: "prod-ap-5", Namespaces: []string{"storefront"},
		Accounts:            []*consolev1.EnrollmentAccount{{Id: "alex", Roles: []string{"viewer"}}},
		ApproverClusterRole: "ptah-operator-ptah-operator-approver",
	})
	c.Assert(err, qt.IsNil)
	file, err := enrollment.Parse(created.GetEnrollmentFile())
	c.Assert(err, qt.IsNil)
	file.Console.Address = "https://" + agents
	agentNamespace := "agent-" + file.Identity.ConnectionUID[:8]
	c.Assert((&enrollment.Helper{
		File: file, Kube: plane.Env.Config, ContextName: "envtest-admin", Namespace: agentNamespace, Release: "ptah-console-agent",
		ValuesPath: filepath.Join(t.TempDir(), "values.yaml"), Out: io.Discard,
	}).Enroll(t.Context()), qt.IsNil)
	secret := &corev1.Secret{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: agentNamespace, Name: "ptah-console-agent-credential"}, secret), qt.IsNil)
	credential := string(secret.Data["credential"])
	scope := consolev1connect.NewScopeServiceClient(r.transport())
	activate := &consolev1.ActivateEnrollmentRequest{ClusterName: "prod-ap-5"}

	_, err = connections.ActivateEnrollment(to(t.Context(), alex, ""), activate)
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeFailedPrecondition, qt.Commentf("activated before the agent connected: %v", err))

	runAgent(t, agents, certificates, file.Identity.ConnectionUID, plane.Env.Config, "[alex]", credential)
	pending := &consolev1alpha1.ConsoleClusterEnrollment{}
	eventually(t, "the console seeing the agent of the pending enrollment", func() bool {
		c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: r.namespace, Name: "prod-ap-5"}, pending), qt.IsNil)
		return meta.IsStatusConditionTrue(pending.Status.Conditions, consolev1alpha1.ConditionAgentConnected)
	})
	_, err = scope.ListNamespaces(to(t.Context(), alex, file.Identity.ConnectionUID), &consolev1.ListNamespacesRequest{})
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeNotFound, qt.Commentf("a pending enrollment was dispatched to: %v", err))
	_, err = connections.ActivateEnrollment(to(t.Context(), r.signIn(t, "sam@example.com", "sam-secret"), ""), activate)
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied, qt.Commentf("%v", err))

	activated, err := connections.ActivateEnrollment(to(t.Context(), alex, ""), activate)

	c.Assert(err, qt.IsNil)
	c.Assert(activated.GetConnectionUid(), qt.Equals, file.Identity.ConnectionUID)
	entry := &consolev1alpha1.ConsoleCluster{}
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKey{Namespace: r.namespace, Name: "prod-ap-5"}, entry), qt.IsNil)
	c.Assert(entry.Spec.Agent.TargetServer, qt.Equals, strings.TrimSuffix(plane.Env.Config.Host, "/"))
	c.Assert(entry.Status.Channel.Credentials[0].Verifier, qt.Equals, enrollment.Verifier(credential))
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(pending), pending), qt.Satisfies, apierrors.IsNotFound)
	eventually(t, "a request reaching the new connection's agent", func() bool {
		_, err := scope.ListNamespaces(to(t.Context(), alex, file.Identity.ConnectionUID), &consolev1.ListNamespacesRequest{})
		return err == nil
	})
}

// eventually waits until check holds, and fails the test after ten seconds.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
