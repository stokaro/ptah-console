package hosted

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/agentlink"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/enrollment"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/server"
	"github.com/stokaro/ptah-console/internal/wire"
)

// tendInterval is how often the console tends its agents' credentials.
const tendInterval = time.Minute

// agentsProblem says why the options cannot serve agents.
func (o Options) agentsProblem(config *accounts.Config) error {
	files := o.AgentTLSCertFile != "" || o.AgentTLSKeyFile != ""
	switch {
	case o.AgentListen == "" && files:
		return errors.New("--agent-tls-cert-file and --agent-tls-key-file serve agents, which needs --agent-listen")
	case o.AgentListen == "":
		return nil
	case config.Registry == nil:
		return errors.New("--agent-listen serves the agents of the registry's connections; the configuration names no registry")
	case config.Registry.Agents == nil:
		return errors.New("--agent-listen needs registry.agents in the configuration: the address, name and CA an enrollment gives agents")
	case o.AgentTLSCertFile == "" || o.AgentTLSKeyFile == "":
		return errors.New("--agent-listen needs --agent-tls-cert-file and --agent-tls-key-file: the console's certificate from its own CA")
	}
	return nil
}

// agents is the listener agents dial and what serves it. The browser's
// services are not on it, and the agent service is on nothing else, so a
// browser session never reaches an agent endpoint and a channel credential
// never reaches a browser endpoint.
type agents struct {
	listener net.Listener
	server   *http.Server
	hub      *agentlink.Hub
}

// listenForAgents opens the agents' listener, or returns nil when the
// options name none.
func (o Options) listenForAgents(kubeConfig *rest.Config, config *accounts.Config) (*agents, error) {
	if o.AgentListen == "" {
		return nil, nil
	}
	certificate := &certificateFiles{certFile: o.AgentTLSCertFile, keyFile: o.AgentTLSKeyFile, logger: o.Logger}
	if _, err := certificate.get(nil); err != nil {
		return nil, fmt.Errorf("load the agent listener's certificate: %w", err)
	}
	housekeeping, err := kube.NewClient(kubeConfig)
	if err != nil {
		return nil, err
	}
	hub := agentlink.NewHub(housekeeping, config.Registry.Namespace, o.Now, o.Logger)
	rpc := connect.NewServer()
	agentv1connect.RegisterAgentServiceHandler(rpc, hub)
	agentv1connect.RegisterEnrollmentServiceHandler(rpc, enrollment.NewService(housekeeping, config.Registry.Namespace, o.Now, o.Audit))
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpc, connecthttp.WithReadMaxBytes(agent.MaxFrameBytes), connecthttp.WithSendMaxBytes(agent.MaxFrameBytes))
	httpServer := server.NewHTTPServer(mux, &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: certificate.get}, o.Logger)
	// The channel is a bidirectional stream, which HTTP/1.1 cannot carry.
	httpServer.Protocols = new(http.Protocols)
	httpServer.Protocols.SetHTTP2(true)
	ping := o.AgentPing
	if ping == 0 {
		ping = wire.Ping
	}
	httpServer.HTTP2 = &http.HTTP2Config{SendPingTimeout: ping, PingTimeout: ping}
	listener, err := net.Listen("tcp", o.AgentListen)
	if err != nil {
		return nil, fmt.Errorf("listen for agents on %s: %w", o.AgentListen, err)
	}
	return &agents{listener: listener, server: httpServer, hub: hub}, nil
}

// serve serves agents and tends their credentials until ctx ends.
func (a *agents) serve(ctx context.Context, housekeeping *rest.Config, namespace string, now func() time.Time, logger *slog.Logger) (<-chan error, error) {
	client, err := kube.NewClient(housekeeping)
	if err != nil {
		return nil, err
	}
	go agentlink.TendAll(ctx, client, namespace, tendInterval, now, logger)
	go enrollment.TendAll(ctx, client, namespace, tendInterval, now, logger)
	return server.Serve(ctx, a.server, []net.Listener{a.listener}), nil
}
