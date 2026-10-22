// Package agent_test holds the console agent to the API server's verdict: a
// request runs as the allowlisted account's one username and nobody else,
// and the agent's own grant is what limits whom it may act as.
package agent_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/audit"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{OperatorCRDs: true}, &plane))
}

const (
	agentUser = "ptah-console-agent"
	alex      = "ptah-console:prod:alex"
	ceiling   = `apiVersion: console.ptah.run/v1alpha1
kind: AgentConfiguration
installationID: prod
connection:
  uid: 3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f
  generation: 1
console:
  address: https://127.0.0.1
  serverName: example.com
namespaces: [storefront, billing]
`
	allowlist = `apiVersion: console.ptah.run/v1alpha1
kind: AgentAccounts
accounts: [alex, sam]
`
)

// arranged is the agent's own identity once its grants exist: impersonate
// alex's username only, which is narrower than its allowlist on purpose, and
// alex may read schemas in storefront and nothing in billing.
var arranged = sync.OnceValues(func() (*rest.Config, error) {
	ctx := context.Background()
	objects := []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "storefront"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "billing"}},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-impersonate"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""}, Resources: []string{"users"}, Verbs: []string{"impersonate"}, ResourceNames: []string{alex},
			}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-impersonate"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "agent-impersonate"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: agentUser}},
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: "storefront", Name: "viewer"},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas"}, Verbs: []string{"get", "list"},
			}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: "storefront", Name: "viewer"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "viewer"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: alex}},
		},
	}
	for _, object := range objects {
		if err := plane.Admin.Create(ctx, object); err != nil {
			return nil, err
		}
	}
	return plane.User(agentUser)
})

func newDispatcher(t *testing.T) *agent.Dispatcher {
	t.Helper()
	c := qt.New(t)
	own, err := arranged()
	c.Assert(err, qt.IsNil)
	config, err := agent.ParseConfiguration([]byte(ceiling))
	c.Assert(err, qt.IsNil)
	factory, err := agent.NewFactory(own, config)
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte(allowlist))
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Set(list), qt.IsNil)
	return agent.NewDispatcher(factory, audit.New(io.Discard), nil)
}

func request(id, account string) *agentv1.Request {
	return &agentv1.Request{
		Envelope: &agentv1.Envelope{RequestId: id, AccountId: account, Deadline: durationpb.New(10 * time.Second)},
		Action:   &agentv1.Request_WhoAmI{WhoAmI: &agentv1.WhoAmIRequest{}},
	}
}

func serve(t *testing.T, dispatcher *agent.Dispatcher, request *agentv1.Request) *agentv1.Result {
	t.Helper()
	c := qt.New(t)
	var results []*agentv1.Result
	c.Assert(dispatcher.Serve(t.Context(), request, func(result *agentv1.Result) error {
		results = append(results, result)
		return nil
	}), qt.IsNil)
	c.Assert(results, qt.HasLen, 1)
	return results[0]
}

// The API server says the agent acted as alex's username, with only the
// group it gives every authenticated user.
func TestTheAgentActsAsTheAccountsUsername(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	result := serve(t, newDispatcher(t), request("r1", "alex"))

	c.Assert(result.GetFailure(), qt.IsNil)
	c.Assert(result.GetWhoAmI().GetUsername(), qt.Equals, alex)
	c.Assert(result.GetWhoAmI().GetGroups(), qt.DeepEquals, []string{"system:authenticated"})
}

// An account on the allowlist that the agent's own grant does not cover is
// refused by the API server: the grant is the second fence.
func TestTheAgentsGrantLimitsWhomItMayActAs(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	result := serve(t, newDispatcher(t), request("r1", "sam"))

	c.Assert(result.GetFailure().GetCode(), qt.Equals, "permission_denied")
	c.Assert(result.GetFailure().GetMessage(), qt.Equals, "the agent may not act as the account's username")
}

// A read runs as alex: allowed where alex may read, forbidden where alex may
// not, whatever the agent's own identity may do.
func TestAReadIsTheAccountsOwn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	list := request("r1", "alex")
	list.Action = &agentv1.Request_ListSchemas{ListSchemas: &consolev1.ListSchemasRequest{Namespaces: []string{"storefront", "billing"}}}

	result := serve(t, newDispatcher(t), list)

	c.Assert(result.GetFailure(), qt.IsNil)
	access := make(map[string]consolev1.Access)
	for _, namespace := range result.GetListSchemas().GetNamespaces() {
		access[namespace.GetNamespace()] = namespace.GetAccess()
	}
	c.Assert(access, qt.DeepEquals, map[string]consolev1.Access{
		"storefront": consolev1.Access_ACCESS_OK,
		"billing":    consolev1.Access_ACCESS_FORBIDDEN,
	})
}

// The whole path: a console sends a request down the channel the agent
// opened, and the answer comes back as alex.
type console struct {
	agentv1connect.UnimplementedAgentServiceHandler

	answers chan *agentv1.Result
}

func (c *console) Channel(_ context.Context, stream agentv1connect.AgentServiceChannelServerStream) error {
	hello, err := stream.Receive()
	if err != nil {
		return err
	}
	frame := &agentv1.ChannelResponse{Binding: hello.GetBinding(), Body: &agentv1.ChannelResponse_Request{Request: request("over-the-channel", "alex")}}
	if err := stream.Send(frame); err != nil {
		return err
	}
	answer, err := stream.Receive()
	if err != nil {
		return err
	}
	c.answers <- answer.GetResult()
	return nil
}

func TestARequestCrossesTheChannelAndRunsAsTheAccount(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	handler := &console{answers: make(chan *agentv1.Result, 1)}
	rpc := connect.NewServer()
	agentv1connect.RegisterAgentServiceHandler(rpc, handler)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpc)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	config, err := agent.ParseConfiguration([]byte(ceiling))
	c.Assert(err, qt.IsNil)
	config.Console.Address = server.URL
	roots := server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs

	go func() {
		_ = agent.Open(t.Context(), agent.ChannelOptions{
			Config: config, RootCAs: roots, Credential: func() (string, error) { return "for-tests-only", nil },
		}, newDispatcher(t))
	}()

	result := <-handler.answers
	c.Assert(result.GetRequestId(), qt.Equals, "over-the-channel")
	c.Assert(result.GetWhoAmI().GetUsername(), qt.Equals, alex)
}
