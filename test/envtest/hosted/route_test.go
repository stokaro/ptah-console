package hosted_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/agentlink"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/hosted"
	"github.com/stokaro/ptah-console/internal/route"
	"github.com/stokaro/ptah-console/test/freeze"
)

// The connections of the routing rows: this cluster, two external clusters
// whose agents are connected, and one alex and sam were not granted.
const (
	routeThis   = "6c3d4e5f-7081-4c9d-8eaf-2a3b4c5d6e7f"
	routeA      = "7d4e5f60-8192-4dae-9fb0-3b4c5d6e7f80"
	routeB      = "8e5f6071-92a3-4ebf-a0c1-4c5d6e7f8091"
	routeHidden = "9f607182-a3b4-4fc0-b1d2-5d6e7f8091a2"
)

// standIn stands in for cluster B's API server. It records every request
// that is not discovery and answers 503.
type standIn struct {
	mu   sync.Mutex
	seen []string
}

var standInDiscovery = map[string]string{
	"/api": `{"kind":"APIVersions","versions":["v1"]}`,
	"/apis": `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"operator.ptah.run",` +
		`"versions":[{"groupVersion":"operator.ptah.run/v1alpha1","version":"v1alpha1"}],` +
		`"preferredVersion":{"groupVersion":"operator.ptah.run/v1alpha1","version":"v1alpha1"}}]}`,
	"/apis/operator.ptah.run/v1alpha1": `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"operator.ptah.run/v1alpha1","resources":[` +
		`{"name":"ptahschemas","namespaced":true,"kind":"PtahSchema","verbs":["get","list","watch"]},` +
		`{"name":"ptahschemaapprovals","namespaced":true,"kind":"PtahSchemaApproval","verbs":["get","list","create"]}]}`,
}

func (s *standIn) RoundTrip(request *http.Request) (*http.Response, error) {
	if body, ok := standInDiscovery[request.URL.Path]; ok {
		return &http.Response{StatusCode: http.StatusOK, Request: request,
			Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	s.mu.Lock()
	s.seen = append(s.seen, request.Method+" "+request.URL.Path)
	s.mu.Unlock()
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Request: request,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (s *standIn) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

// hostAs sends every request with the console's public host, which the
// console holds a request's Host to.
type hostAs struct{}

func (hostAs) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Host = "console.envtest"
	return http.DefaultTransport.RoundTrip(request)
}

// routing is a hosted console with two external clusters connected.
type routing struct {
	browser   string
	namespace string
	b         *standIn
	// ping, when set, is the console's agent ping; frozen, when set, carries
	// cluster-a's agent through a proxy that start sets it to.
	ping   time.Duration
	frozen **freeze.Proxy
}

func arrangeRouting(t *testing.T) *routing {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	group := consolev1alpha1.GroupVersion.Group
	registryNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "route-"}}
	c.Assert(plane.Admin.Create(ctx, registryNamespace), qt.IsNil)
	ns := registryNamespace.Name
	entry := func(name, uid string, profile consolev1alpha1.ConnectionProfile) *consolev1alpha1.ConsoleCluster {
		spec := consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: uid, Profile: profile, InstallationID: "envtest",
			Namespaces: []string{"storefront"}, ConfigurationOwner: consolev1alpha1.OwnerConsole,
		}
		if profile == consolev1alpha1.ProfileAgent {
			spec.Agent = &consolev1alpha1.AgentTrust{
				TargetServer: "https://api." + name + ".example.com:6443", TargetCASHA256: "sha256:" + strings.Repeat("1", 64),
				ConsoleServerName: agentsName, ConsoleCASHA256: "sha256:" + strings.Repeat("2", 64),
			}
		}
		return &consolev1alpha1.ConsoleCluster{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Spec: spec}
	}
	people := []rbacv1.Subject{
		{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: alexUsername},
		{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: samUsername},
	}
	for _, object := range []client.Object{
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "housekeeping"},
			Rules:      housekeepingRules(),
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "housekeeping"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "housekeeping"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "granted-connections"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{group}, Resources: []string{"consoleclusters"}, Verbs: []string{"get"},
				ResourceNames: []string{"this-cluster", "cluster-a", "cluster-b"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "granted-connections"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "granted-connections"},
			Subjects:   people,
		},
		entry("this-cluster", routeThis, consolev1alpha1.ProfileCoLocated),
		entry("cluster-a", routeA, consolev1alpha1.ProfileAgent),
		entry("cluster-b", routeB, consolev1alpha1.ProfileAgent),
		entry("cluster-hidden", routeHidden, consolev1alpha1.ProfileAgent),
	} {
		c.Assert(plane.Admin.Create(ctx, object), qt.IsNil)
	}
	storefront := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "storefront"}}
	if err := plane.Admin.Create(ctx, storefront); err != nil && !apierrors.IsAlreadyExists(err) {
		c.Fatal(err)
	}
	reader := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: "storefront", GenerateName: "schema-reader-"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas"}, Verbs: []string{"get", "list"},
		}},
	}
	c.Assert(plane.Admin.Create(ctx, reader), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: "storefront", GenerateName: "schema-reader-"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: reader.Name},
		Subjects:   people,
	}), qt.IsNil)
	return &routing{namespace: ns, b: &standIn{}}
}

// start runs the console and both agents, and waits until both channels are
// open.
func (r *routing) start(t *testing.T) {
	t.Helper()
	c := qt.New(t)
	keyA, keyB := client.ObjectKey{Namespace: r.namespace, Name: "cluster-a"}, client.ObjectKey{Namespace: r.namespace, Name: "cluster-b"}
	credentialA, err := agentlink.Issue(t.Context(), plane.Admin, keyA, time.Now(), agentlink.DefaultValidity)
	c.Assert(err, qt.IsNil)
	credentialB, err := agentlink.Issue(t.Context(), plane.Admin, keyB, time.Now(), agentlink.DefaultValidity)
	c.Assert(err, qt.IsNil)
	certificates := newPKI(t)
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
			AgentListening: func(addr net.Addr) { agents <- addr }, AgentPing: r.ping,
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	r.browser = (<-browser).String()
	agentAddress := (<-agents).String()

	addressA := agentAddress
	if r.frozen != nil {
		*r.frozen = freeze.Start(t, agentAddress)
		addressA = (*r.frozen).Addr()
	}
	runAgent(t, addressA, certificates, routeA, plane.Env.Config, "[alex, sam]", credentialA)
	runAgent(t, agentAddress, certificates, routeB, &rest.Config{Host: "https://cluster-b.invalid", Transport: r.b, QPS: -1}, "[alex]", credentialB)
	for _, key := range []client.ObjectKey{keyA, keyB} {
		deadline := time.Now().Add(10 * time.Second)
		entry := &consolev1alpha1.ConsoleCluster{}
		for !meta.IsStatusConditionTrue(entry.Status.Conditions, consolev1alpha1.ConditionConnected) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			c.Assert(plane.Admin.Get(t.Context(), key, entry), qt.IsNil)
		}
		c.Assert(meta.IsStatusConditionTrue(entry.Status.Conditions, consolev1alpha1.ConditionConnected), qt.IsTrue, qt.Commentf("%s never connected", key))
	}
}

// runAgent holds an agent's channel open for the test, acting through base.
func runAgent(t *testing.T, address string, certificates *pki, connection string, base *rest.Config, allowlisted, credential string) {
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
	factory, err := agent.NewFactory(base, config)
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte("apiVersion: console.ptah.run/v1alpha1\nkind: AgentAccounts\naccounts: " + allowlisted + "\n"))
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
	t.Cleanup(func() {
		cancel()
		<-ended
	})
}

// signIn signs a person in and returns a client of the console's services
// that sends their cookie.
func (r *routing) signIn(t *testing.T, login, password string) *http.Cookie {
	t.Helper()
	c := qt.New(t)
	response := callConsole(t, r.browser, "SessionService/Login", `{"login":"`+login+`","password":"`+password+`"}`)
	c.Assert(response.StatusCode, qt.Equals, http.StatusOK)
	c.Assert(response.Cookies(), qt.HasLen, 1)
	return response.Cookies()[0]
}

func (r *routing) transport() *connect.Client {
	return connect.NewClient(connecthttp.NewTransport(&http.Client{Transport: hostAs{}}, "http://"+r.browser+"/api"))
}

// to is a call as the cookie's person, for the connection.
func to(ctx context.Context, cookie *http.Cookie, connection string) context.Context {
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Cookie", cookie.Name+"="+cookie.Value)
	if connection != "" {
		info.RequestHeader().Set(route.Header, connection)
	}
	return ctx
}

// A request goes to the cluster its connection names and to no other: two
// clusters with the same namespace and schema names cannot be confused, and
// a decision for one never reaches the other.
func TestARequestReachesOnlyTheClusterItNames(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	shop := r.schema(t, "shop")
	r.start(t)
	alex := r.signIn(t, "alex@example.com", "alex-secret")
	schemas := consolev1connect.NewSchemaServiceClient(r.transport())
	decisions := consolev1connect.NewDecisionServiceClient(r.transport())

	fromA, err := schemas.GetSchema(to(t.Context(), alex, routeA), &consolev1.GetSchemaRequest{Namespace: "storefront", Name: "shop"})
	c.Assert(err, qt.IsNil)
	c.Assert(fromA.GetSchema().GetRef().GetName(), qt.Equals, "shop")
	c.Assert(r.b.requests(), qt.HasLen, 0, qt.Commentf("a request for cluster A reached cluster B"))

	_, err = schemas.GetSchema(to(t.Context(), alex, routeB), &consolev1.GetSchemaRequest{Namespace: "storefront", Name: "shop"})
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeUnavailable, qt.Commentf("cluster B's stand-in answers 503: %v", err))
	c.Assert(r.b.requests(), qt.Contains, "GET /apis/operator.ptah.run/v1alpha1/namespaces/storefront/ptahschemas/shop")

	before := len(r.b.requests())
	_, err = decisions.ApproveSchemaPlan(to(t.Context(), alex, routeB), &consolev1.ApproveSchemaPlanRequest{
		Namespace: "storefront", SchemaName: "shop", SchemaUid: string(shop.UID), PlanName: "shop-plan", PlanUid: "plan-uid",
		PlanFingerprint: "sha256:" + strings.Repeat("3", 64), ContractVersion: 1,
	})
	c.Assert(err, qt.IsNotNil)
	c.Assert(len(r.b.requests()) > before, qt.IsTrue, qt.Commentf("the decision for cluster B never reached it"))
	approvals := &operatorv1alpha1.PtahSchemaApprovalList{}
	c.Assert(plane.Admin.List(t.Context(), approvals, client.InNamespace("storefront")), qt.IsNil)
	c.Assert(approvals.Items, qt.HasLen, 0, qt.Commentf("the decision for cluster B reached cluster A"))
}

// An account one agent allows and another does not sees nothing of the
// other cluster; a connection a person was not granted reads as one that
// does not exist; and the cluster the console runs in is served as before.
func TestEachClusterDecidesWhoSeesIt(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	r.start(t)
	sam := r.signIn(t, "sam@example.com", "sam-secret")
	scope := consolev1connect.NewScopeServiceClient(r.transport())

	_, err := scope.ListNamespaces(to(t.Context(), sam, routeA), &consolev1.ListNamespacesRequest{})
	c.Assert(err, qt.IsNil)
	_, err = scope.ListNamespaces(to(t.Context(), sam, routeB), &consolev1.ListNamespacesRequest{})
	c.Assert(connect.CodeOf(err), qt.Equals, connect.CodePermissionDenied)
	c.Assert(err, qt.ErrorMatches, `.*account "sam" is not on this agent's allowlist`)
	c.Assert(r.b.requests(), qt.HasLen, 0)

	for _, connection := range []string{routeHidden, "00000000-0000-4000-8000-000000000000"} {
		_, err = scope.ListNamespaces(to(t.Context(), sam, connection), &consolev1.ListNamespacesRequest{})
		c.Assert(connect.CodeOf(err), qt.Equals, connect.CodeNotFound, qt.Commentf("%s", connection))
		c.Assert(err, qt.ErrorMatches, `.*no connection with that UID is visible to you`)
	}

	local, err := scope.ListNamespaces(to(t.Context(), sam, routeThis), &consolev1.ListNamespacesRequest{})
	c.Assert(err, qt.IsNil)
	c.Assert(local.GetNamespaces(), qt.Not(qt.HasLen), 0)
}

// schema is the shop schema in storefront on cluster A, created once.
func (r *routing) schema(t *testing.T, name string) *operatorv1alpha1.PtahSchema {
	t.Helper()
	c := qt.New(t)
	shop := schemaFixture("storefront", name)
	err := plane.Admin.Create(t.Context(), shop)
	c.Assert(err == nil || apierrors.IsAlreadyExists(err), qt.IsTrue, qt.Commentf("%v", err))
	c.Assert(plane.Admin.Get(t.Context(), client.ObjectKeyFromObject(shop), shop), qt.IsNil)
	return shop
}

// schemaFixture is a schema the operator's CRD accepts, with the operator
// example's values.
func schemaFixture(namespace, name string) *operatorv1alpha1.PtahSchema {
	return &operatorv1alpha1.PtahSchema{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: operatorv1alpha1.PtahSchemaSpec{
			Target: operatorv1alpha1.DatabaseTargetSpec{
				Engine:          operatorv1alpha1.DatabaseEnginePostgreSQL,
				CoordinationKey: "envtest/" + namespace,
				URLFrom: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-database"}, Key: "url",
				},
			},
			Desired: operatorv1alpha1.SourceSpec{
				OCIRef: "oci://registry.example/shop/schema:1.0.0",
				VerificationPolicyFrom: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "ptah-verification-policy"}, Key: "policy.yaml",
				},
			},
			Policy:   operatorv1alpha1.ReconciliationPolicy{Apply: operatorv1alpha1.ApplyPolicyOnApproval, LockTimeout: metav1.Duration{Duration: 30 * time.Second}},
			Interval: metav1.Duration{Duration: 10 * time.Minute},
			Execution: operatorv1alpha1.ExecutionSpec{
				FailureRetryInterval: metav1.Duration{Duration: 30 * time.Second},
				ConnectTimeout:       metav1.Duration{Duration: 10 * time.Second},
			},
		},
	}
}

// A frozen agent's channel ends once a ping goes unanswered, and a request
// for its cluster is then refused at once as not connected, not sent into a
// channel nothing reads. A live agent answers the pings and keeps its
// channel.
func TestAFrozenAgentIsDisconnected(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	r := arrangeRouting(t)
	var proxy *freeze.Proxy
	r.ping, r.frozen = 250*time.Millisecond, &proxy
	r.start(t)
	alex := r.signIn(t, "alex@example.com", "alex-secret")
	scope := consolev1connect.NewScopeServiceClient(r.transport())
	time.Sleep(8 * r.ping)
	_, err := scope.ListNamespaces(to(t.Context(), alex, routeA), &consolev1.ListNamespacesRequest{})
	c.Assert(err, qt.IsNil, qt.Commentf("the pings ended a live channel"))

	proxy.Freeze()

	eventually(t, "the console refusing the frozen agent's cluster as not connected", func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		_, err := scope.ListNamespaces(to(ctx, alex, routeA), &consolev1.ListNamespacesRequest{})
		return connect.CodeOf(err) == connect.CodeUnavailable && strings.Contains(err.Error(), "the cluster's agent is not connected")
	})
}
