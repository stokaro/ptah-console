// Package agentlink_test runs the console's hub and a real agent against a
// real API server: credentials the console issues and keeps only verifiers
// of, channels bound to a connection and its generation, and rotation.
package agentlink_test

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/agentlink"
	"github.com/stokaro/ptah-console/internal/audit"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{RegistryCRDs: true, OperatorCRDs: true}, &plane))
}

const sha = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// clock is the hub's clock, which a test moves.
type clock struct{ at atomic.Pointer[time.Time] }

func newClock(at time.Time) *clock {
	c := &clock{}
	c.set(at)
	return c
}

func (c *clock) now() time.Time   { return *c.at.Load() }
func (c *clock) set(at time.Time) { c.at.Store(&at) }

// world is a registry namespace with one Agent entry, a hub serving it over
// TLS, and what an agent needs to reach the hub.
type world struct {
	t       *testing.T
	key     client.ObjectKey
	uid     string
	hub     *agentlink.Hub
	clock   *clock
	address string
	roots   *x509.CertPool
}

func newWorld(t *testing.T, uid string) *world {
	t.Helper()
	c := qt.New(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "agentlink-"}}
	c.Assert(plane.Admin.Create(t.Context(), namespace), qt.IsNil)
	entry := &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "prod-ap-2"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: uid, Profile: consolev1alpha1.ProfileAgent, InstallationID: "prod",
			Namespaces: []string{"storefront"}, ConfigurationOwner: consolev1alpha1.OwnerConsole,
			Agent: &consolev1alpha1.AgentTrust{
				TargetServer: "https://api.prod-ap-2.example.com:6443", TargetCASHA256: sha,
				ConsoleServerName: "example.com", ConsoleCASHA256: sha,
			},
		},
	}
	c.Assert(plane.Admin.Create(t.Context(), entry), qt.IsNil)
	w := &world{t: t, key: client.ObjectKeyFromObject(entry), uid: uid, clock: newClock(time.Now().Truncate(time.Second))}
	w.hub = agentlink.NewHub(plane.Admin, namespace.Name, w.clock.now, nil)
	rpc := connect.NewServer()
	agentv1connect.RegisterAgentServiceHandler(rpc, w.hub)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpc)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	transport, ok := server.Client().Transport.(*http.Transport)
	c.Assert(ok, qt.IsTrue)
	w.address, w.roots = server.URL, transport.TLSClientConfig.RootCAs
	return w
}

func (w *world) issue() string {
	w.t.Helper()
	credential, err := agentlink.Issue(w.t.Context(), plane.Admin, w.key, w.clock.now(), agentlink.DefaultValidity)
	qt.New(w.t).Assert(err, qt.IsNil)
	return credential
}

// retireAt is when the console stops accepting credential, if it has said.
func (w *world) retireAt(credential string) *metav1.Time {
	w.t.Helper()
	for _, kept := range w.entry().Status.Channel.Credentials {
		if kept.Verifier == agentlink.Verifier(credential) {
			return kept.RetireAt
		}
	}
	return nil
}

func (w *world) entry() *consolev1alpha1.ConsoleCluster {
	w.t.Helper()
	entry := &consolev1alpha1.ConsoleCluster{}
	qt.New(w.t).Assert(plane.Admin.Get(w.t.Context(), w.key, entry), qt.IsNil)
	return entry
}

// credentialFile is an agent's mounted Secret, which a test rewrites.
type credentialFile struct{ value atomic.Pointer[string] }

func (f *credentialFile) set(value string) { f.value.Store(&value) }
func (f *credentialFile) read() (string, error) {
	return *f.value.Load(), nil
}

// agentRun is an agent holding its channel open, and how it ended.
type agentRun struct {
	cancel context.CancelFunc
	ended  chan error
}

// open runs one channel attempt of an agent bound to generation, with the
// credential the file holds.
func (w *world) open(file *credentialFile, generation uint64) *agentRun {
	w.t.Helper()
	c := qt.New(w.t)
	config, err := agent.ParseConfiguration([]byte(`apiVersion: console.ptah.run/v1alpha1
kind: AgentConfiguration
installationID: prod
connection: {uid: ` + w.uid + `, generation: 1}
console: {address: https://127.0.0.1, serverName: example.com}
namespaces: [storefront]
`))
	c.Assert(err, qt.IsNil)
	config.Console.Address = w.address
	config.Connection.Generation = generation
	factory, err := agent.NewFactory(plane.Env.Config, config)
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte("apiVersion: console.ptah.run/v1alpha1\nkind: AgentAccounts\naccounts: [alex]\n"))
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Set(list), qt.IsNil)
	ctx, cancel := context.WithCancel(w.t.Context())
	run := &agentRun{cancel: cancel, ended: make(chan error, 1)}
	options := agent.ChannelOptions{Config: config, RootCAs: w.roots, Credential: file.read, CredentialCheck: 20 * time.Millisecond}
	go func() {
		run.ended <- agent.Open(ctx, options, agent.NewDispatcher(factory, audit.New(io.Discard), nil))
	}()
	w.t.Cleanup(cancel)
	return run
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// whoAmI asks the connected agent whom alex acts as, and returns every result.
func (w *world) whoAmI() []*agentv1.Result {
	w.t.Helper()
	c := qt.New(w.t)
	pending, err := w.hub.Send(w.t.Context(), w.uid, &agentv1.Request{
		Envelope: &agentv1.Envelope{AccountId: "alex", Deadline: durationpb.New(10 * time.Second), SentAt: timestamppb.Now()},
		Action:   &agentv1.Request_WhoAmI{WhoAmI: &agentv1.WhoAmIRequest{}},
	})
	c.Assert(err, qt.IsNil)
	var results []*agentv1.Result
	for result := range pending.Results {
		results = append(results, result)
	}
	return results
}

func file(value string) *credentialFile {
	f := &credentialFile{}
	f.set(value)
	return f
}

// An agent with the credential the console issued opens the channel bound
// to its connection, and a request sent down it runs as the account. The
// console keeps the credential's verifier, never the credential.
func TestAnIssuedCredentialOpensTheChannel(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e61")
	credential := w.issue()

	w.open(file(credential), 1)
	eventually(t, "the channel opening", func() bool { return w.hub.Connected(w.uid) })
	results := w.whoAmI()

	c.Assert(results, qt.HasLen, 1)
	c.Assert(results[0].GetLast(), qt.IsTrue)
	c.Assert(results[0].GetWhoAmI().GetUsername(), qt.Equals, "ptah-console:prod:alex")
	entry := w.entry()
	c.Assert(entry.Status.Channel.Generation, qt.Equals, int64(1))
	c.Assert(entry.Status.Channel.Credentials, qt.HasLen, 1)
	c.Assert(entry.Status.Channel.Credentials[0].Verifier, qt.Equals, agentlink.Verifier(credential))
	c.Assert(entry.Status.Channel.Credentials[0].Verifier, qt.Not(qt.Equals), credential)
	c.Assert(entry.Status.Channel.Credentials[0].FirstUsedAt, qt.IsNotNil)
	c.Assert(meta.IsStatusConditionTrue(entry.Status.Conditions, consolev1alpha1.ConditionConnected), qt.IsTrue)
}

// A credential the console does not accept opens nothing, and every refusal
// reads the same.
func TestARefusedCredentialOpensNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(w *world) string
	}{
		{name: "one the console never issued", setup: func(w *world) string {
			w.issue()
			return "Zm9yLXRlc3RzLW9ubHktbm90LWlzc3VlZA"
		}},
		{name: "an expired one", setup: func(w *world) string {
			credential := w.issue()
			w.clock.set(w.clock.now().Add(agentlink.DefaultValidity))
			return credential
		}},
		{name: "the verifier in place of the credential", setup: func(w *world) string {
			return agentlink.Verifier(w.issue())
		}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			w := newWorld(t, fmt.Sprintf("3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e%02d", 70+index))
			presented := test.setup(w)

			err := <-w.open(file(presented), 1).ended

			c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeUnauthenticated)
			c.Assert(err, qt.ErrorMatches, `.*the credential is not one this console accepts`)
			c.Assert(w.hub.Connected(w.uid), qt.IsFalse)
		})
	}
}

// An agent that binds its channel to another generation is refused.
func TestAChannelBoundToAnotherGenerationIsRefused(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e80")
	credential := w.issue()

	err := <-w.open(file(credential), 2).ended

	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied)
	c.Assert(err, qt.ErrorMatches, `.*generation 2; its credential is for .* generation 1`)
}

// Raising the generation ends the channel, and the agent's next channel,
// still bound to the old generation, is refused.
func TestRaisingTheGenerationFencesTheAgent(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e81")
	credential := w.issue()
	first := w.open(file(credential), 1)
	eventually(t, "the channel opening", func() bool { return w.hub.Connected(w.uid) })

	generation, err := agentlink.Raise(t.Context(), plane.Admin, w.key)
	c.Assert(err, qt.IsNil)
	w.hub.End(w.uid)

	c.Assert(generation, qt.Equals, int64(2))
	c.Assert(<-first.ended, qt.IsNotNil)
	err = <-w.open(file(credential), 1).ended
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied)
}

// A newer channel of the same connection ends the older one.
func TestANewerChannelReplacesTheOlder(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e82")
	credential := w.issue()
	first := w.open(file(credential), 1)
	eventually(t, "the first channel opening", func() bool { return w.hub.Connected(w.uid) })

	w.open(file(credential), 1)

	c.Assert(<-first.ended, qt.IsNotNil)
	eventually(t, "the second channel serving", func() bool { return len(w.whoAmI()) == 1 })
}

// alexMayWatch lets alex's username watch schemas, so a watch window stays
// open for its whole deadline.
var alexMayWatch = sync.OnceValue(func() error {
	ctx := context.Background()
	for _, object := range []client.Object{
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "alex-watch"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas"}, Verbs: []string{"list", "watch"},
			}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "alex-watch"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "alex-watch"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "ptah-console:prod:alex"}},
		},
	} {
		if err := plane.Admin.Create(ctx, object); err != nil {
			return err
		}
	}
	return nil
})

// A request whose channel ends before its answer is lost: its results end
// without a last one, and the connection has no channel to send it down
// again.
func TestARequestOnADroppedChannelIsLost(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	c.Assert(alexMayWatch(), qt.IsNil)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e83")
	run := w.open(file(w.issue()), 1)
	eventually(t, "the channel opening", func() bool { return w.hub.Connected(w.uid) })
	watch := &agentv1.Request{
		Envelope: &agentv1.Envelope{AccountId: "alex", Deadline: durationpb.New(30 * time.Second), SentAt: timestamppb.Now()},
		Action:   &agentv1.Request_Watch{Watch: &consolev1.SubscribeRequest{Namespaces: []string{"storefront"}}},
	}
	pending, err := w.hub.Send(t.Context(), w.uid, watch)
	c.Assert(err, qt.IsNil)
	first, open := <-pending.Results
	c.Assert(open, qt.IsTrue, qt.Commentf("the watch ended before it reported anything"))
	c.Assert(first.GetLast(), qt.IsFalse)

	run.cancel()
	var last bool
	for result := range pending.Results {
		last = last || result.GetLast()
	}

	c.Assert(last, qt.IsFalse)
	eventually(t, "the hub forgetting the channel", func() bool { return !w.hub.Connected(w.uid) })
	_, err = w.hub.Send(t.Context(), w.uid, watch)
	c.Assert(err, qt.ErrorIs, agentlink.ErrNotConnected)
}

// A rotation: the agent's Secret gets the second credential, the agent opens
// its channel again with it, and the first is accepted for at most an hour
// after that.
func TestARotationRetiresTheOldCredentialAfterTheNewOneIsUsed(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e84")
	old := w.issue()
	secret := file(old)
	run := w.open(secret, 1)
	eventually(t, "the channel opening", func() bool { return w.hub.Connected(w.uid) })
	w.clock.set(w.clock.now().Add(80 * 24 * time.Hour))
	rotated := w.issue()
	_, err := agentlink.Issue(t.Context(), plane.Admin, w.key, w.clock.now(), agentlink.DefaultValidity)
	c.Assert(err, qt.ErrorIs, agentlink.ErrRotationInProgress)

	secret.set(rotated)

	c.Assert(<-run.ended, qt.ErrorIs, agent.ErrCredentialRotated)
	w.open(secret, 1)
	eventually(t, "the old credential's retirement", func() bool { return w.retireAt(old) != nil })
	retireAt := w.retireAt(old).Time
	c.Assert(retireAt, qt.Equals, w.clock.now().Add(agentlink.RotationOverlap))

	w.clock.set(retireAt.Add(-time.Second))
	stillAccepted := w.open(file(old), 1)
	eventually(t, "the old credential opening a channel within the hour", func() bool { return len(w.whoAmI()) == 1 })
	stillAccepted.cancel()
	w.clock.set(retireAt)
	err = <-w.open(file(old), 1).ended
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeUnauthenticated)

	c.Assert(agentlink.Tend(t.Context(), plane.Admin, w.key, w.clock.now()), qt.IsNil)
	credentials := w.entry().Status.Channel.Credentials
	c.Assert(credentials, qt.HasLen, 1)
	c.Assert(credentials[0].Verifier, qt.Equals, agentlink.Verifier(rotated))
}

// The console says a credential in use is expiring from 14 days before it
// does.
func TestTheConsoleSaysWhenTheCredentialExpires(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e85")
	issuedAt := w.clock.now()
	w.issue()
	notAfter := issuedAt.Add(agentlink.DefaultValidity)

	c.Assert(agentlink.Tend(t.Context(), plane.Admin, w.key, notAfter.Add(-agentlink.ExpiryWarning-time.Second)), qt.IsNil)
	before := meta.FindStatusCondition(w.entry().Status.Conditions, consolev1alpha1.ConditionRotationDue)
	c.Assert(agentlink.Tend(t.Context(), plane.Admin, w.key, notAfter.Add(-agentlink.ExpiryWarning)), qt.IsNil)
	after := meta.FindStatusCondition(w.entry().Status.Conditions, consolev1alpha1.ConditionRotationDue)

	c.Assert(before.Status, qt.Equals, metav1.ConditionFalse)
	c.Assert(after.Status, qt.Equals, metav1.ConditionTrue)
	c.Assert(after.Message, qt.Contains, "rotate it")
}

// The console tends its agents' credentials on its own: an expired one is
// dropped and the warning is written without anyone asking.
func TestTheConsoleTendsItsAgentsOnItsOwn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	w := newWorld(t, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e86")
	w.issue()
	w.clock.set(w.clock.now().Add(agentlink.DefaultValidity - agentlink.ExpiryWarning))

	go agentlink.TendAll(t.Context(), plane.Admin, w.key.Namespace, 10*time.Millisecond, w.clock.now, slog.New(slog.DiscardHandler))

	eventually(t, "the expiry warning", func() bool {
		return meta.IsStatusConditionTrue(w.entry().Status.Conditions, consolev1alpha1.ConditionRotationDue)
	})
	w.clock.set(w.clock.now().Add(agentlink.ExpiryWarning))
	eventually(t, "the expired credential's removal", func() bool { return len(w.entry().Status.Channel.Credentials) == 0 })
	c.Assert(w.entry().Status.Channel.Generation, qt.Equals, int64(1))
}
