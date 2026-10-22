package agent_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	qt "github.com/frankban/quicktest"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/stokaro/ptah-console/internal/agent"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/test/freeze"
)

const (
	consoleName = "agents.console.test"
	presented   = "for-tests-only"
)

// authority is a CA that issues the console's certificates in a test.
type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newAuthority(t *testing.T) *authority {
	t.Helper()
	c := qt.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c.Assert(err, qt.IsNil)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "console CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	c.Assert(err, qt.IsNil)
	cert, err := x509.ParseCertificate(der)
	c.Assert(err, qt.IsNil)
	return &authority{cert: cert, key: key}
}

func (a *authority) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)
	return pool
}

func (a *authority) issue(t *testing.T, name string) tls.Certificate {
	t.Helper()
	c := qt.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c.Assert(err, qt.IsNil)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	c.Assert(err, qt.IsNil)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// console stands in for the console's agent listener. It reports each
// Authorization header it receives, and runs script on the channel.
type console struct {
	agentv1connect.UnimplementedAgentServiceHandler

	authorizations chan string
	script         func(ctx context.Context, stream agentv1connect.AgentServiceChannelServerStream) error
}

func (c *console) Channel(ctx context.Context, stream agentv1connect.AgentServiceChannelServerStream) error {
	info, _ := connect.CallInfoForServerContext(ctx)
	c.authorizations <- info.RequestHeader().Get("Authorization")
	return c.script(ctx, stream)
}

// step is one thing the stand-in console does on a channel.
type step func(ctx context.Context, stream agentv1connect.AgentServiceChannelServerStream) error

// script runs steps in order and stops at the first that fails.
func script(steps ...step) func(context.Context, agentv1connect.AgentServiceChannelServerStream) error {
	return func(ctx context.Context, stream agentv1connect.AgentServiceChannelServerStream) error {
		for _, next := range steps {
			if err := next(ctx, stream); err != nil {
				return err
			}
		}
		return nil
	}
}

// receive hands the agent's next frame to into.
func receive(into chan<- *agentv1.ChannelRequest) step {
	return func(_ context.Context, stream agentv1connect.AgentServiceChannelServerStream) error {
		frame, err := stream.Receive()
		if err != nil {
			return err
		}
		into <- frame
		return nil
	}
}

// sendFrame sends the agent a frame.
func sendFrame(frame *agentv1.ChannelResponse) step {
	return func(_ context.Context, stream agentv1connect.AgentServiceChannelServerStream) error {
		return stream.Send(frame)
	}
}

// waitFor runs a check that blocks until something has happened.
func waitFor(check func()) step {
	return func(context.Context, agentv1connect.AgentServiceChannelServerStream) error {
		check()
		return nil
	}
}

// holdOpen keeps the channel open until the agent closes it.
func holdOpen(ctx context.Context, _ agentv1connect.AgentServiceChannelServerStream) error {
	<-ctx.Done()
	return nil
}

func serveConsole(t *testing.T, certificate tls.Certificate, maxVersion uint16, handler *console) string {
	t.Helper()
	rpc := connect.NewServer()
	agentv1connect.RegisterAgentServiceHandler(rpc, handler)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpc)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, MaxVersion: maxVersion}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server.URL
}

func channelOptions(t *testing.T, address string, roots *x509.CertPool) agent.ChannelOptions {
	t.Helper()
	config, err := agent.ParseConfiguration([]byte(configuration))
	qt.New(t).Assert(err, qt.IsNil)
	config.Console = agent.Console{Address: address, ServerName: consoleName}
	return agent.ChannelOptions{
		Config: config, RootCAs: roots, Version: "test",
		Credential: func() (string, error) { return presented, nil },
	}
}

// open runs one channel in the background and returns how it ended.
func open(t *testing.T, options agent.ChannelOptions, dispatcher *agent.Dispatcher) <-chan error {
	t.Helper()
	ended := make(chan error, 1)
	go func() { ended <- agent.Open(t.Context(), options, dispatcher) }()
	return ended
}

func binding(generation uint64) *agentv1.Binding {
	return &agentv1.Binding{ConnectionUid: "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f", ConnectionGeneration: generation}
}

// The agent proves itself to the console it was enrolled with, says hello
// for its connection, answers a request, and closes the channel when the
// console names another generation.
func TestTheAgentServesTheChannelOfItsConnection(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ca := newAuthority(t)
	hello := make(chan *agentv1.ChannelRequest, 1)
	answer := make(chan *agentv1.ChannelRequest, 1)
	handler := &console{authorizations: make(chan string, 1), script: script(
		receive(hello),
		sendFrame(&agentv1.ChannelResponse{Binding: binding(1), Body: &agentv1.ChannelResponse_Request{Request: whoAmI("r1", "lee")}}),
		receive(answer),
		sendFrame(&agentv1.ChannelResponse{Binding: binding(2), Body: &agentv1.ChannelResponse_Cancel{Cancel: &agentv1.Cancel{RequestId: "r1"}}}),
		holdOpen,
	)}
	address := serveConsole(t, ca.issue(t, consoleName), tls.VersionTLS13, handler)
	dispatcher, _ := newDispatcher(t, &apiServer{})

	ended := open(t, channelOptions(t, address, ca.pool()), dispatcher)

	c.Assert(<-handler.authorizations, qt.Equals, "Bearer "+presented)
	first := <-hello
	c.Assert(first.GetBinding().GetConnectionUid(), qt.Equals, "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f")
	c.Assert(first.GetBinding().GetConnectionGeneration(), qt.Equals, uint64(1))
	c.Assert(first.GetHello().GetProtocol(), qt.Equals, uint32(agent.Protocol))
	result := (<-answer).GetResult()
	c.Assert(result.GetRequestId(), qt.Equals, "r1")
	c.Assert(result.GetFailure().GetCode(), qt.Equals, "permission_denied")
	c.Assert(<-ended, qt.ErrorMatches, `the console sent a frame for connection 3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f generation 2`)
}

// A console that cannot prove it is the enrolled one never sees the
// credential: the handshake fails before any request is made.
func TestTheAgentSendsNoCredentialToAnotherServer(t *testing.T) {
	t.Parallel()
	ca := newAuthority(t)
	tests := []struct {
		name        string
		certificate func(t *testing.T) tls.Certificate
		maxVersion  uint16
		says        string
	}{
		{name: "a certificate from another CA", certificate: func(t *testing.T) tls.Certificate {
			return newAuthority(t).issue(t, consoleName)
		}, maxVersion: tls.VersionTLS13, says: `.*certificate signed by unknown authority.*`},
		{name: "a certificate for another name", certificate: func(t *testing.T) tls.Certificate {
			return ca.issue(t, "console.example.com")
		}, maxVersion: tls.VersionTLS13, says: `.*certificate is valid for console.example.com, not agents.console.test.*`},
		{name: "TLS 1.2", certificate: func(t *testing.T) tls.Certificate {
			return ca.issue(t, consoleName)
		}, maxVersion: tls.VersionTLS12, says: `.*protocol version.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			handler := &console{authorizations: make(chan string, 1), script: func(context.Context, agentv1connect.AgentServiceChannelServerStream) error {
				return nil
			}}
			address := serveConsole(t, test.certificate(t), test.maxVersion, handler)
			dispatcher, _ := newDispatcher(t, &apiServer{})

			err := <-open(t, channelOptions(t, address, ca.pool()), dispatcher)

			c.Assert(err, qt.ErrorMatches, test.says)
			c.Assert(handler.authorizations, qt.HasLen, 0)
		})
	}
}

// A request the console cancels ends at once, and says so.
func TestTheConsoleCancelsARequest(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ca := newAuthority(t)
	api := &apiServer{hold: make(chan struct{})}
	t.Cleanup(func() { close(api.hold) })
	hello := make(chan *agentv1.ChannelRequest, 1)
	answer := make(chan *agentv1.ChannelRequest, 1)
	request := whoAmI("r1", "alex")
	request.Action = &agentv1.Request_GetSchema{GetSchema: &consolev1.GetSchemaRequest{Namespace: "storefront", Name: "shop"}}
	request.Envelope.Deadline = durationpb.New(30 * time.Second)
	handler := &console{authorizations: make(chan string, 1), script: script(
		receive(hello),
		sendFrame(&agentv1.ChannelResponse{Binding: binding(1), Body: &agentv1.ChannelResponse_Request{Request: request}}),
		waitFor(func() { api.waitForRequests(t, 1) }),
		sendFrame(&agentv1.ChannelResponse{Binding: binding(1), Body: &agentv1.ChannelResponse_Cancel{Cancel: &agentv1.Cancel{RequestId: "r1"}}}),
		receive(answer),
	)}
	address := serveConsole(t, ca.issue(t, consoleName), tls.VersionTLS13, handler)
	dispatcher, _ := newDispatcher(t, api)

	open(t, channelOptions(t, address, ca.pool()), dispatcher)

	result := (<-answer).GetResult()
	c.Assert(result.GetRequestId(), qt.Equals, "r1")
	c.Assert(result.GetLast(), qt.IsTrue)
	c.Assert(result.GetFailure().GetCode(), qt.Equals, "canceled")
}

// An agent told to stop ends its channel at once, with a request in flight
// and the console still holding the channel open.
func TestAnAgentToldToStopEndsItsChannel(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ca := newAuthority(t)
	api := &apiServer{hold: make(chan struct{})}
	t.Cleanup(func() { close(api.hold) })
	hello := make(chan *agentv1.ChannelRequest, 1)
	request := whoAmI("r1", "alex")
	request.Action = &agentv1.Request_GetSchema{GetSchema: &consolev1.GetSchemaRequest{Namespace: "storefront", Name: "shop"}}
	handler := &console{authorizations: make(chan string, 1), script: script(
		receive(hello),
		sendFrame(&agentv1.ChannelResponse{Binding: binding(1), Body: &agentv1.ChannelResponse_Request{Request: request}}),
		holdOpen,
	)}
	address := serveConsole(t, ca.issue(t, consoleName), tls.VersionTLS13, handler)
	dispatcher, _ := newDispatcher(t, api)
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan error, 1)
	go func() { ended <- agent.Open(ctx, channelOptions(t, address, ca.pool()), dispatcher) }()
	<-hello
	api.waitForRequests(t, 1)

	cancel()

	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		c.Fatal("the agent was still waiting for the console five seconds after it was told to stop")
	}
}

// An agent whose console stops answering, frozen or cut off without a word,
// ends its channel once its ping goes unanswered, so it dials again instead
// of waiting minutes for TCP to give up. A console that answers keeps it.
func TestAnAgentNoticesAConsoleThatStoppedAnswering(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ca := newAuthority(t)
	hello := make(chan *agentv1.ChannelRequest, 1)
	handler := &console{authorizations: make(chan string, 1), script: script(receive(hello), holdOpen)}
	address := serveConsole(t, ca.issue(t, consoleName), tls.VersionTLS13, handler)
	proxy := freeze.Start(t, strings.TrimPrefix(address, "https://"))
	options := channelOptions(t, "https://"+proxy.Addr(), ca.pool())
	options.Ping = 250 * time.Millisecond
	dispatcher, _ := newDispatcher(t, &apiServer{})
	ended := open(t, options, dispatcher)
	<-hello
	select {
	case err := <-ended:
		c.Fatalf("the agent ended a channel its console kept answering: %v", err)
	case <-time.After(8 * options.Ping):
	}

	proxy.Freeze()

	select {
	case err := <-ended:
		c.Assert(err, qt.IsNotNil)
	case <-time.After(10 * time.Second):
		c.Fatal("the agent still held its channel to a console that stopped answering ten seconds before")
	}
}

// A console that stops answering with a request in flight ends the channel
// as one with none does: the request is canceled, and the agent dials again.
func TestAnAgentNoticesAStoppedConsoleWithARequestInFlight(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ca := newAuthority(t)
	api := &apiServer{hold: make(chan struct{})}
	t.Cleanup(func() { close(api.hold) })
	hello := make(chan *agentv1.ChannelRequest, 1)
	request := whoAmI("r1", "alex")
	request.Action = &agentv1.Request_GetSchema{GetSchema: &consolev1.GetSchemaRequest{Namespace: "storefront", Name: "shop"}}
	request.Envelope.Deadline = durationpb.New(30 * time.Second)
	handler := &console{authorizations: make(chan string, 1), script: script(
		receive(hello),
		sendFrame(&agentv1.ChannelResponse{Binding: binding(1), Body: &agentv1.ChannelResponse_Request{Request: request}}),
		holdOpen,
	)}
	address := serveConsole(t, ca.issue(t, consoleName), tls.VersionTLS13, handler)
	proxy := freeze.Start(t, strings.TrimPrefix(address, "https://"))
	options := channelOptions(t, "https://"+proxy.Addr(), ca.pool())
	options.Ping = 250 * time.Millisecond
	dispatcher, _ := newDispatcher(t, api)
	ended := open(t, options, dispatcher)
	<-hello
	api.waitForRequests(t, 1)

	proxy.Freeze()

	select {
	case err := <-ended:
		c.Assert(err, qt.IsNotNil)
	case <-time.After(10 * time.Second):
		c.Fatal("the agent still held its channel, and the request in it, ten seconds after its console stopped answering")
	}
}
