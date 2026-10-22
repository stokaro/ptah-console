package agent_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"google.golang.org/protobuf/types/known/durationpb"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/audit"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
)

// apiServer stands in for the API server. It records the headers of every
// request that reaches it, and holds each until released when hold is set.
type apiServer struct {
	hold chan struct{}

	mu   sync.Mutex
	seen []http.Header
}

// discovery is what the stand-in API server says it serves: enough for a
// client to map the kinds a held request uses, so discovery is never what
// holds it.
var discovery = map[string]string{
	"/api": `{"kind":"APIVersions","versions":["v1"]}`,
	"/apis": `{"kind":"APIGroupList","apiVersion":"v1","groups":[` +
		`{"name":"authentication.k8s.io","versions":[{"groupVersion":"authentication.k8s.io/v1","version":"v1"}],` +
		`"preferredVersion":{"groupVersion":"authentication.k8s.io/v1","version":"v1"}},` +
		`{"name":"operator.ptah.run","versions":[{"groupVersion":"operator.ptah.run/v1alpha1","version":"v1alpha1"}],` +
		`"preferredVersion":{"groupVersion":"operator.ptah.run/v1alpha1","version":"v1alpha1"}}]}`,
	"/apis/authentication.k8s.io/v1": `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"authentication.k8s.io/v1",` +
		`"resources":[{"name":"selfsubjectreviews","namespaced":false,"kind":"SelfSubjectReview","verbs":["create"]}]}`,
	"/apis/operator.ptah.run/v1alpha1": `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"operator.ptah.run/v1alpha1",` +
		`"resources":[{"name":"ptahschemas","namespaced":true,"kind":"PtahSchema","verbs":["get","list","watch"]}]}`,
}

func (a *apiServer) RoundTrip(request *http.Request) (*http.Response, error) {
	if body, ok := discovery[request.URL.Path]; ok {
		return &http.Response{
			StatusCode: http.StatusOK, Request: request,
			Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	}
	a.mu.Lock()
	a.seen = append(a.seen, request.Header.Clone())
	a.mu.Unlock()
	if a.hold != nil {
		select {
		case <-a.hold:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	}
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable, Request: request,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{}")),
	}, nil
}

func (a *apiServer) requests() []http.Header {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]http.Header(nil), a.seen...)
}

// waitForRequests waits until n requests have reached the API server.
func (a *apiServer) waitForRequests(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(a.requests()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d requests reached the API server, not %d", len(a.requests()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newDispatcher(t *testing.T, api *apiServer) (*agent.Dispatcher, *agent.Factory) {
	t.Helper()
	c := qt.New(t)
	config, err := agent.ParseConfiguration([]byte(configuration))
	c.Assert(err, qt.IsNil)
	// No client-side rate limit, so the bounds under test are the agent's.
	factory, err := agent.NewFactory(&rest.Config{Host: "https://kube.invalid", Transport: api, QPS: -1}, config)
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte(allowlist))
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Set(list), qt.IsNil)
	return agent.NewDispatcher(factory, audit.New(io.Discard), nil), factory
}

func whoAmI(id, account string) *agentv1.Request {
	return &agentv1.Request{
		Envelope: &agentv1.Envelope{
			RequestId: id, ConsoleInstance: "console-0", SessionGeneration: 1, AccountId: account,
			Deadline: durationpb.New(5 * time.Second),
		},
		Action: &agentv1.Request_WhoAmI{WhoAmI: &agentv1.WhoAmIRequest{}},
	}
}

func listSchemas(id, account string, namespaces ...string) *agentv1.Request {
	request := whoAmI(id, account)
	request.Action = &agentv1.Request_ListSchemas{ListSchemas: &consolev1.ListSchemasRequest{Namespaces: namespaces}}
	return request
}

// serve runs one request and returns every result it sent.
func serve(t *testing.T, dispatcher *agent.Dispatcher, request *agentv1.Request) []*agentv1.Result {
	t.Helper()
	var results []*agentv1.Result
	err := dispatcher.Serve(t.Context(), request, func(result *agentv1.Result) error {
		results = append(results, result)
		return nil
	})
	qt.New(t).Assert(err, qt.IsNil)
	return results
}

// failed is the code and message a request's one result failed with.
func failed(t *testing.T, results []*agentv1.Result) (code, message string) {
	t.Helper()
	c := qt.New(t)
	c.Assert(results, qt.HasLen, 1)
	c.Assert(results[0].GetLast(), qt.IsTrue)
	return results[0].GetFailure().GetCode(), results[0].GetFailure().GetMessage()
}

// A request the agent will not serve is refused before anything reaches the
// API server.
func TestEachRefusalComesBeforeAnyCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		request *agentv1.Request
		code    string
		says    string
	}{
		{name: "no envelope", request: &agentv1.Request{Action: &agentv1.Request_WhoAmI{WhoAmI: &agentv1.WhoAmIRequest{}}},
			code: "invalid_argument", says: "a request needs an envelope"},
		{name: "a request ID with a space", request: whoAmI("a b", "alex"), code: "invalid_argument", says: "the request ID is not one"},
		{name: "an overlong request ID", request: whoAmI(strings.Repeat("a", 65), "alex"), code: "invalid_argument", says: "the request ID is not one"},
		{name: "no deadline", request: func() *agentv1.Request {
			request := whoAmI("r1", "alex")
			request.Envelope.Deadline = nil
			return request
		}(), code: "invalid_argument", says: "a request needs a deadline of at most 30s"},
		{name: "a deadline past the bound", request: func() *agentv1.Request {
			request := whoAmI("r1", "alex")
			request.Envelope.Deadline = durationpb.New(31 * time.Second)
			return request
		}(), code: "invalid_argument", says: "a request needs a deadline of at most 30s"},
		{name: "a deadline already past", request: func() *agentv1.Request {
			request := whoAmI("r1", "alex")
			request.Envelope.Deadline = durationpb.New(-time.Second)
			return request
		}(), code: "invalid_argument", says: "a request needs a deadline of at most 30s"},
		{name: "an account the allowlist does not name", request: whoAmI("r1", "lee"),
			code: "permission_denied", says: `account "lee" is not on this agent's allowlist`},
		{name: "a username in place of an account", request: whoAmI("r1", "ptah-console:prod:alex"),
			code: "permission_denied", says: `account "ptah-console:prod:alex" is not on this agent's allowlist`},
		{name: "a system name in place of an account", request: whoAmI("r1", "system:masters"),
			code: "permission_denied", says: `account "system:masters" is not on this agent's allowlist`},
		{name: "a namespace outside the ceiling", request: listSchemas("r1", "alex", "storefront", "kube-system"),
			code: "permission_denied", says: `namespace "kube-system" is outside what this agent serves`},
		{name: "an approval outside the ceiling", request: func() *agentv1.Request {
			request := whoAmI("r1", "alex")
			request.Action = &agentv1.Request_ApproveSchemaPlan{ApproveSchemaPlan: &consolev1.ApproveSchemaPlanRequest{Namespace: "kube-system"}}
			return request
		}(), code: "permission_denied", says: `namespace "kube-system" is outside what this agent serves`},
		{name: "no operation", request: func() *agentv1.Request {
			request := whoAmI("r1", "alex")
			request.Action = nil
			return request
		}(), code: "unimplemented", says: "the agent serves no such request"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			api := &apiServer{}
			dispatcher, _ := newDispatcher(t, api)

			code, message := failed(t, serve(t, dispatcher, test.request))

			c.Assert(code, qt.Equals, test.code)
			c.Assert(message, qt.Equals, test.says)
			c.Assert(api.requests(), qt.HasLen, 0)
		})
	}
}

// A request the agent serves reaches the API server as the account's one
// username, with no group, UID or extra, whatever else the console sent.
func TestARequestActsAsTheAccountsUsernameAndNobodyElse(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	api := &apiServer{}
	dispatcher, _ := newDispatcher(t, api)

	code, _ := failed(t, serve(t, dispatcher, whoAmI("r1", "alex")))

	c.Assert(code, qt.Equals, "unavailable", qt.Commentf("the stand-in API server answers 503"))
	requests := api.requests()
	c.Assert(requests, qt.HasLen, 1)
	c.Assert(impersonationOf(requests[0]), qt.DeepEquals, http.Header{"Impersonate-User": {"ptah-console:prod:alex"}})
}

// impersonationOf is the impersonation headers of a request.
func impersonationOf(header http.Header) http.Header {
	out := make(http.Header)
	for key, values := range header {
		if strings.HasPrefix(key, "Impersonate-") {
			out[key] = values
		}
	}
	return out
}

// A request ID is served once; the second request that carries it reaches
// nothing.
func TestARepeatedRequestIDIsRefused(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	api := &apiServer{}
	dispatcher, _ := newDispatcher(t, api)
	serve(t, dispatcher, whoAmI("r1", "alex"))

	code, message := failed(t, serve(t, dispatcher, whoAmI("r1", "alex")))

	c.Assert(code, qt.Equals, "already_exists")
	c.Assert(message, qt.Equals, "request r1 was already received")
	c.Assert(api.requests(), qt.HasLen, 1)
}

// An account taken off the allowlist is refused from the next request on,
// before any call.
func TestAnAccountTakenOffTheAllowlistIsRefused(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	api := &apiServer{}
	dispatcher, factory := newDispatcher(t, api)
	serve(t, dispatcher, whoAmI("r1", "alex"))
	list, err := agent.ParseAccounts([]byte(strings.Replace(allowlist, "[alex, sam]", "[sam]", 1)))
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Set(list), qt.IsNil)
	dispatcher.Revoke()

	code, _ := failed(t, serve(t, dispatcher, whoAmI("r2", "alex")))

	c.Assert(code, qt.Equals, "permission_denied")
	c.Assert(api.requests(), qt.HasLen, 1)
	_, err = factory.For(factory.Principal("alex"))
	c.Assert(err, qt.ErrorMatches, `agent: account "alex" is not on the allowlist`)
}

// One account has at most MaxPerAccount requests in flight, and another
// account is not held behind it.
func TestOneAccountCannotTakeTheWholeConnection(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	api := &apiServer{hold: make(chan struct{})}
	dispatcher, _ := newDispatcher(t, api)
	var running sync.WaitGroup
	for index := range agent.MaxPerAccount {
		running.Go(func() { serve(t, dispatcher, listSchemas(fmt.Sprintf("held-%d", index), "alex", "storefront")) })
	}
	api.waitForRequests(t, agent.MaxPerAccount)

	code, message := failed(t, serve(t, dispatcher, listSchemas("one-more", "alex", "storefront")))
	running.Go(func() { serve(t, dispatcher, listSchemas("another-account", "sam", "storefront")) })
	api.waitForRequests(t, agent.MaxPerAccount+1)
	close(api.hold)
	running.Wait()

	c.Assert(code, qt.Equals, "resource_exhausted")
	c.Assert(message, qt.Equals, "this session has too many requests in flight")
}

// The connection has at most MaxInFlight requests in flight.
func TestTheConnectionHoldsItsBound(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	api := &apiServer{hold: make(chan struct{})}
	dispatcher, _ := newDispatcher(t, api)
	var running sync.WaitGroup
	for index := range agent.MaxInFlight {
		running.Go(func() { serve(t, dispatcher, whoAmI(fmt.Sprintf("held-%d", index), "alex")) })
	}
	api.waitForRequests(t, agent.MaxInFlight)

	code, message := failed(t, serve(t, dispatcher, whoAmI("one-more", "sam")))
	close(api.hold)
	running.Wait()

	c.Assert(code, qt.Equals, "resource_exhausted")
	c.Assert(message, qt.Equals, "the agent has 64 requests in flight")
	c.Assert(api.requests(), qt.HasLen, agent.MaxInFlight)
}
