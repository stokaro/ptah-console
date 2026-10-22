// Command probe opens the agent channel the way an agent does, with what it
// is given, and says what the console answered. The two-cluster acceptance
// rows use it to present the console with what a real agent never would: a
// credential bound to another connection, another protocol, a bootstrap
// secret in place of a credential.
//
// It prints one line: "open" when the console kept the channel for the whole
// wait, or "refused <code>: <message>" when it ended it. A channel the console
// keeps replaces the connection's live one, so a row probes with a binding or
// a credential the console has to refuse.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	"github.com/stokaro/ptah-console/internal/wire"
)

// options is what to present to the listener.
type options struct {
	address, serverName, caFile, credentialFile, connection string
	generation                                              uint64
	protocol                                                uint
	wait                                                    time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.address, "address", "", "the https root of the console's agent listener")
	flag.StringVar(&o.serverName, "server-name", "", "the name the listener's certificate carries")
	flag.StringVar(&o.caFile, "ca", "", "the console's CA, in PEM")
	flag.StringVar(&o.credentialFile, "credential-file", "", "a file holding what to present as the bearer")
	flag.StringVar(&o.connection, "connection", "", "the connection UID to bind the channel to")
	flag.Uint64Var(&o.generation, "generation", 1, "the connection generation to bind the channel to")
	flag.UintVar(&o.protocol, "protocol", wire.Protocol, "the protocol to say hello with")
	flag.DurationVar(&o.wait, "wait", 10*time.Second, "how long an open channel is held before it counts as open")
	flag.Parse()
	answer, err := run(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, answer)
}

func run(o options) (string, error) {
	if o.address == "" || o.serverName == "" || o.caFile == "" || o.credentialFile == "" || o.connection == "" {
		return "", errors.New("-address, -server-name, -ca, -credential-file and -connection are required")
	}
	ca, err := os.ReadFile(o.caFile)
	if err != nil {
		return "", err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return "", fmt.Errorf("%s holds no certificate", o.caFile)
	}
	presented, err := os.ReadFile(o.credentialFile)
	if err != nil {
		return "", err
	}
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, ServerName: o.serverName, MinVersion: tls.VersionTLS13},
		ForceAttemptHTTP2: true,
	}
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP2(true)
	defer transport.CloseIdleConnections()
	client := agentv1connect.NewAgentServiceClient(connect.NewClient(connecthttp.NewTransport(&http.Client{Transport: transport}, o.address)))

	ctx, cancel := context.WithTimeout(context.Background(), o.wait)
	defer cancel()
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Authorization", "Bearer "+strings.TrimSpace(string(presented)))
	stream, err := client.Channel(ctx)
	if err != nil {
		return outcome(ctx, err), nil
	}
	defer func() { _ = stream.Close() }()
	err = stream.Send(&agentv1.ChannelRequest{
		Binding: &agentv1.Binding{ConnectionUid: o.connection, ConnectionGeneration: o.generation},
		Body:    &agentv1.ChannelRequest_Hello{Hello: &agentv1.Hello{Protocol: uint32(o.protocol), AgentVersion: "probe"}}, // #nosec G115 -- a flag value.
	})
	if err == nil {
		_, err = stream.Receive()
	}
	return outcome(ctx, err), nil
}

// outcome is what the console's answer reads as. The wait running out with
// the channel still up is the console keeping it.
func outcome(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "open"
	}
	return fmt.Sprintf("refused %s: %s", connect.CodeOf(err), connectMessage(err))
}

func connectMessage(err error) string {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr.Message()
	}
	return err.Error()
}
