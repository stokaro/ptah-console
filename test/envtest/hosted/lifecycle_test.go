package hosted_test

import (
	"context"
	"io"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	qt "github.com/frankban/quicktest"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/agentlink"
	"github.com/stokaro/ptah-console/internal/audit"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

// grantConnectionAdmin gives alex what the chart's connection-admin role
// grants, and the console its revocation rights.
func grantConnectionAdmin(t *testing.T, namespace string) {
	t.Helper()
	c := qt.New(t)
	group := consolev1alpha1.GroupVersion.Group
	for _, object := range []client.Object{
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "lifecycle-admin"},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterpreferences"},
					Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
				{APIGroups: []string{group}, Resources: []string{"consoleclusterrevocations"}, Verbs: []string{"get", "list", "watch"}},
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "lifecycle-admin"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "lifecycle-admin"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: alexUsername}},
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "lifecycle-housekeeping"},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{group}, Resources: []string{"consoleclusterpreferences"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{group}, Resources: []string{"consoleclusterrevocations"}, Verbs: []string{"get", "list", "watch", "create"}},
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "lifecycle-housekeeping"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "lifecycle-housekeeping"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
		},
	} {
		c.Assert(plane.Admin.Create(t.Context(), object), qt.IsNil)
	}
}

// startAgent holds an agent's channel open, opening it again when it drops,
// until the returned stop is called.
func startAgent(t *testing.T, address string, certificates *pki, connection string, generation uint64, credential string) (stop func()) {
	t.Helper()
	c := qt.New(t)
	config, err := agent.ParseConfiguration([]byte(`apiVersion: console.ptah.run/v1alpha1
kind: AgentConfiguration
installationID: envtest
connection: {uid: ` + connection + `, generation: 1}
console: {address: https://` + address + `, serverName: ` + agentsName + `}
namespaces: [storefront]
`))
	c.Assert(err, qt.IsNil)
	config.Connection.Generation = generation
	factory, err := agent.NewFactory(plane.Env.Config, config)
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte("apiVersion: console.ptah.run/v1alpha1\nkind: AgentAccounts\naccounts: [alex, sam]\n"))
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Set(list), qt.IsNil)
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_ = agent.Run(ctx, agent.ChannelOptions{
			Config: config, RootCAs: certificates.roots, Credential: func() (string, error) { return credential, nil },
		}, agent.NewDispatcher(factory, audit.New(io.Discard), nil))
	}()
	stop = func() {
		cancel()
		<-ended
	}
	t.Cleanup(stop)
	return stop
}

// Disabling a connection stops every request to it and keeps it; enabling
// it serves again. Quarantine ends the channel and refuses it, a GitOps
// reapply of the old record does not lift it, and new credentials at a
// higher generation do.
func TestDisableAndQuarantine(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	grantEnrollments(t, r.namespace)
	grantConnectionAdmin(t, r.namespace)
	certificates := newPKI(t)
	agents := r.startConsole(t, certificates)
	keyA := client.ObjectKey{Namespace: r.namespace, Name: "cluster-a"}
	old, err := agentlink.Issue(t.Context(), plane.Admin, keyA, time.Now(), agentlink.DefaultValidity)
	c.Assert(err, qt.IsNil)
	startAgent(t, agents, certificates, routeA, 1, old)
	alex := r.signIn(t, "alex@example.com", "alex-secret")
	sam := r.signIn(t, "sam@example.com", "sam-secret")
	scope := consolev1connect.NewScopeServiceClient(r.transport())
	connections := consolev1connect.NewConnectionServiceClient(r.transport())
	served := func() error {
		_, err := scope.ListNamespaces(to(t.Context(), alex, routeA), &consolev1.ListNamespacesRequest{})
		return err
	}
	eventually(t, "cluster A serving", func() bool { return served() == nil })

	_, err = connections.SetConnectionEnabled(to(t.Context(), alex, ""), &consolev1.SetConnectionEnabledRequest{ClusterName: "cluster-a", Enabled: false})
	c.Assert(err, qt.IsNil)
	c.Assert(served(), qt.ErrorMatches, `.*connection .* is disabled`)
	_, err = connections.SetConnectionEnabled(to(t.Context(), alex, ""), &consolev1.SetConnectionEnabledRequest{ClusterName: "cluster-a", Enabled: true})
	c.Assert(err, qt.IsNil)
	c.Assert(served(), qt.IsNil)

	_, err = connections.QuarantineConnection(to(t.Context(), sam, ""), &consolev1.QuarantineConnectionRequest{ClusterName: "cluster-a"})
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied, qt.Commentf("%v", err))
	quarantined, err := connections.QuarantineConnection(to(t.Context(), alex, ""), &consolev1.QuarantineConnectionRequest{ClusterName: "cluster-a", Reason: "credential leaked"})
	c.Assert(err, qt.IsNil)
	c.Assert(quarantined.GetGeneration(), qt.Equals, int64(1))
	c.Assert(served(), qt.ErrorMatches, `.*connection .* is quarantined`)

	// A GitOps reapply of the old record: the entry recreated, and its old
	// status restored with it.
	entry := &consolev1alpha1.ConsoleCluster{}
	c.Assert(plane.Admin.Get(t.Context(), keyA, entry), qt.IsNil)
	c.Assert(plane.Admin.Delete(t.Context(), entry), qt.IsNil)
	recreated := &consolev1alpha1.ConsoleCluster{ObjectMeta: metav1.ObjectMeta{Namespace: keyA.Namespace, Name: keyA.Name}, Spec: entry.Spec}
	c.Assert(plane.Admin.Create(t.Context(), recreated), qt.IsNil)
	issued := metav1.NewTime(time.Now().Add(-time.Minute).Truncate(time.Second))
	recreated.Status.Channel = &consolev1alpha1.ChannelStatus{Generation: 1, Credentials: []consolev1alpha1.ChannelCredential{{
		Verifier: agentlink.Verifier(old), IssuedAt: issued, NotAfter: metav1.NewTime(issued.Add(agentlink.DefaultValidity)),
	}}}
	c.Assert(plane.Admin.Status().Update(t.Context(), recreated), qt.IsNil)
	c.Assert(connect.CodeOf(openOnce(t, agents, certificates, routeA, 1, old)), qt.Equals, connect.CodeUnauthenticated,
		qt.Commentf("the reapplied record let the old credential in"))
	c.Assert(served(), qt.ErrorMatches, `.*connection .* is quarantined`, qt.Commentf("the reapplied record lifted the quarantine"))

	lifted, err := connections.IssueChannelCredential(to(t.Context(), alex, ""), &consolev1.IssueChannelCredentialRequest{ClusterName: "cluster-a"})
	c.Assert(err, qt.IsNil)
	c.Assert(lifted.GetGeneration(), qt.Equals, int64(2))
	startAgent(t, agents, certificates, routeA, 2, lifted.GetCredential())
	eventually(t, "cluster A serving again with new credentials", func() bool { return served() == nil })
}

// Removing a connection whose target is offline completes through the
// console and leaves the target's cleanup pending. A recreated record of the
// removed connection revives nothing, and the name, reused, does not inherit
// the old identity.
func TestRemovingAnOfflineConnection(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	grantEnrollments(t, r.namespace)
	grantConnectionAdmin(t, r.namespace)
	certificates := newPKI(t)
	agents := r.startConsole(t, certificates)
	keyB := client.ObjectKey{Namespace: r.namespace, Name: "cluster-b"}
	credential, err := agentlink.Issue(t.Context(), plane.Admin, keyB, time.Now(), agentlink.DefaultValidity)
	c.Assert(err, qt.IsNil)
	alex := r.signIn(t, "alex@example.com", "alex-secret")
	connections := consolev1connect.NewConnectionServiceClient(r.transport())
	scope := consolev1connect.NewScopeServiceClient(r.transport())
	_, err = connections.SetConnectionEnabled(to(t.Context(), alex, ""), &consolev1.SetConnectionEnabledRequest{ClusterName: "cluster-b", Enabled: true})
	c.Assert(err, qt.IsNil)

	removed, err := connections.RemoveConnection(to(t.Context(), alex, ""), &consolev1.RemoveConnectionRequest{ClusterName: "cluster-b", Reason: "decommissioned"})

	c.Assert(err, qt.IsNil)
	c.Assert(removed.GetConnectionUid(), qt.Equals, routeB)
	c.Assert(removed.GetCleanupPending(), qt.IsTrue)
	c.Assert(removed.GetCleanupSteps(), qt.Not(qt.HasLen), 0)
	c.Assert(plane.Admin.Get(t.Context(), keyB, &consolev1alpha1.ConsoleCluster{}), qt.Satisfies, apierrors.IsNotFound)
	c.Assert(plane.Admin.Get(t.Context(), keyB, &consolev1alpha1.ConsoleClusterPreferences{}), qt.Satisfies, apierrors.IsNotFound)
	tombstones := &consolev1alpha1.ConsoleClusterRevocationList{}
	c.Assert(plane.Admin.List(t.Context(), tombstones, client.InNamespace(r.namespace)), qt.IsNil)
	c.Assert(tombstones.Items, qt.HasLen, 1)
	c.Assert(tombstones.Items[0].Spec.Action, qt.Equals, consolev1alpha1.RevocationRemove)
	c.Assert(tombstones.Items[0].Spec.RevokedBy, qt.Equals, "alex")

	// The old record reapplied, with its credential restored: the agent the
	// target never uninstalled is still refused.
	revived := &consolev1alpha1.ConsoleCluster{ObjectMeta: metav1.ObjectMeta{Namespace: keyB.Namespace, Name: keyB.Name}, Spec: consolev1alpha1.ConsoleClusterSpec{
		ConnectionUID: routeB, Profile: consolev1alpha1.ProfileAgent, InstallationID: "envtest", Namespaces: []string{"storefront"},
		ConfigurationOwner: consolev1alpha1.OwnerGitOps,
		Agent: &consolev1alpha1.AgentTrust{
			TargetServer: "https://api.cluster-b.example.com:6443", TargetCASHA256: "sha256:" + routeHash,
			ConsoleServerName: agentsName, ConsoleCASHA256: "sha256:" + routeHash,
		},
	}}
	c.Assert(plane.Admin.Create(t.Context(), revived), qt.IsNil)
	issued := metav1.NewTime(time.Now().Add(-time.Minute).Truncate(time.Second))
	revived.Status.Channel = &consolev1alpha1.ChannelStatus{Generation: 1, Credentials: []consolev1alpha1.ChannelCredential{{
		Verifier: agentlink.Verifier(credential), IssuedAt: issued, NotAfter: metav1.NewTime(issued.Add(agentlink.DefaultValidity)),
	}}}
	c.Assert(plane.Admin.Status().Update(t.Context(), revived), qt.IsNil)
	c.Assert(connect.CodeOf(openOnce(t, agents, certificates, routeB, 1, credential)), qt.Equals, connect.CodeUnauthenticated,
		qt.Commentf("the revived record let the removed connection's credential in"))
	_, err = scope.ListNamespaces(to(t.Context(), alex, routeB), &consolev1.ListNamespacesRequest{})
	c.Assert(err, qt.ErrorMatches, `.*connection .* was removed`)
	_, err = connections.IssueChannelCredential(to(t.Context(), alex, ""), &consolev1.IssueChannelCredentialRequest{ClusterName: "cluster-b"})
	c.Assert(err, qt.ErrorMatches, `.*the connection was removed; enroll a new one`)

	// The name reused by a new connection: a new identity, which the old
	// tombstone does not fence.
	c.Assert(plane.Admin.Delete(t.Context(), revived), qt.IsNil)
	reused := revived.DeepCopy()
	reused.ObjectMeta = metav1.ObjectMeta{Namespace: keyB.Namespace, Name: keyB.Name}
	reused.Spec.ConnectionUID = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	c.Assert(plane.Admin.Create(t.Context(), reused), qt.IsNil)
	fresh, err := connections.IssueChannelCredential(to(t.Context(), alex, ""), &consolev1.IssueChannelCredentialRequest{ClusterName: "cluster-b"})
	c.Assert(err, qt.IsNil)
	c.Assert(fresh.GetGeneration(), qt.Equals, int64(1))
}

// openOnce is one attempt of an agent to open its channel, and how it ended.
func openOnce(t *testing.T, address string, certificates *pki, connection string, generation uint64, credential string) error {
	t.Helper()
	c := qt.New(t)
	config, err := agent.ParseConfiguration([]byte(`apiVersion: console.ptah.run/v1alpha1
kind: AgentConfiguration
installationID: envtest
connection: {uid: ` + connection + `, generation: 1}
console: {address: https://` + address + `, serverName: ` + agentsName + `}
namespaces: [storefront]
`))
	c.Assert(err, qt.IsNil)
	config.Connection.Generation = generation
	factory, err := agent.NewFactory(plane.Env.Config, config)
	c.Assert(err, qt.IsNil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	return agent.Open(ctx, agent.ChannelOptions{
		Config: config, RootCAs: certificates.roots, Credential: func() (string, error) { return credential, nil },
	}, agent.NewDispatcher(factory, audit.New(io.Discard), nil))
}

// routeHash is any SHA-256, for trust fields nothing checks here.
const routeHash = "1111111111111111111111111111111111111111111111111111111111111111"

// A person sees the connections they may get, each with what the console
// observed of it, and whether they may change the registry.
func TestConnectionsShowWhatTheConsoleObserved(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	grantEnrollments(t, r.namespace)
	r.start(t)
	connections := consolev1connect.NewConnectionServiceClient(r.transport())

	alex, err := connections.ListConnections(to(t.Context(), r.signIn(t, "alex@example.com", "alex-secret"), ""), &consolev1.ListConnectionsRequest{})
	c.Assert(err, qt.IsNil)
	sam, err := connections.ListConnections(to(t.Context(), r.signIn(t, "sam@example.com", "sam-secret"), ""), &consolev1.ListConnectionsRequest{})
	c.Assert(err, qt.IsNil)

	states := func(response *consolev1.ListConnectionsResponse) map[string]consolev1.ChannelState {
		out := make(map[string]consolev1.ChannelState)
		for _, connection := range response.GetConnections() {
			out[connection.GetName()] = connection.GetChannel()
		}
		return out
	}
	c.Assert(states(alex), qt.DeepEquals, map[string]consolev1.ChannelState{
		"this-cluster":   consolev1.ChannelState_CHANNEL_STATE_IN_CLUSTER,
		"cluster-a":      consolev1.ChannelState_CHANNEL_STATE_CONNECTED,
		"cluster-b":      consolev1.ChannelState_CHANNEL_STATE_CONNECTED,
		"cluster-hidden": consolev1.ChannelState_CHANNEL_STATE_NEVER_CONNECTED,
	}, qt.Commentf("an administrator sees every connection"))
	c.Assert(states(sam), qt.DeepEquals, map[string]consolev1.ChannelState{
		"this-cluster": consolev1.ChannelState_CHANNEL_STATE_IN_CLUSTER,
		"cluster-a":    consolev1.ChannelState_CHANNEL_STATE_CONNECTED,
		"cluster-b":    consolev1.ChannelState_CHANNEL_STATE_CONNECTED,
	}, qt.Commentf("cluster-hidden is not sam's to see"))
	c.Assert(alex.GetThisCluster(), qt.Equals, routeThis)
	c.Assert(alex.GetManage(), qt.IsTrue)
	c.Assert(sam.GetManage(), qt.IsFalse)
}
