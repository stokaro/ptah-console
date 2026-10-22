package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/stokaro/ptah-operator/pkg/planread"
	"google.golang.org/protobuf/proto"

	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
)

// Protocol is the version of the channel this agent speaks.
const Protocol = 1

// MaxFrameBytes bounds one frame either way: the largest plan the operator
// stores, as a plan response carries it, with room for the envelope. A larger
// frame ends the channel.
const MaxFrameBytes = int(planread.MaxPlanBytes) + 1<<20

// Reconnect backoff.
const (
	firstBackoff = time.Second
	maxBackoff   = 30 * time.Second
)

// ChannelOptions say where the console is and how the agent proves itself.
type ChannelOptions struct {
	Config *Configuration
	// RootCAs is the console's own CA, and nothing else: no system root is
	// trusted for the console.
	RootCAs *x509.CertPool
	// Credential reads the channel credential, again on every attempt, so a
	// rotated Secret is used on the next one.
	Credential func() (string, error)
	Version    string
	Logger     *slog.Logger
}

// Run holds a channel open until ctx ends, opening it again after it drops.
func Run(ctx context.Context, options ChannelOptions, dispatcher *Dispatcher) error {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	backoff := firstBackoff
	for {
		started := time.Now()
		err := Open(ctx, options, dispatcher)
		if ctx.Err() != nil {
			return nil
		}
		logger.Warn("the channel to the console ended", "error", err)
		if time.Since(started) > maxBackoff {
			backoff = firstBackoff
		}
		wait := backoff/2 + rand.N(backoff/2+1) // #nosec G404 -- jitter, not a secret.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		backoff = min(2*backoff, maxBackoff)
	}
}

// Open opens one channel and serves it until it ends. The TLS handshake has
// to verify the console's certificate against RootCAs and the enrolled name
// before the credential is sent.
func Open(ctx context.Context, options ChannelOptions, dispatcher *Dispatcher) error {
	credential, err := options.Credential()
	if err != nil {
		return fmt.Errorf("read the channel credential: %w", err)
	}
	if credential == "" || strings.ContainsAny(credential, " \t\r\n") {
		return errors.New("the channel credential is empty or not one token")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    options.RootCAs,
			ServerName: options.Config.Console.ServerName,
			MinVersion: tls.VersionTLS13,
		},
		ForceAttemptHTTP2: true,
	}
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP2(true)
	defer transport.CloseIdleConnections()
	client := agentv1connect.NewAgentServiceClient(connect.NewClient(connecthttp.NewTransport(
		&http.Client{Transport: transport}, options.Config.Console.Address,
		connecthttp.WithReadMaxBytes(MaxFrameBytes), connecthttp.WithSendMaxBytes(MaxFrameBytes),
	)))
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Authorization", "Bearer "+credential)
	stream, err := client.Channel(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	binding := &agentv1.Binding{
		ConnectionUid: options.Config.Connection.UID, ConnectionGeneration: options.Config.Connection.Generation,
	}
	var sending sync.Mutex
	send := func(frame *agentv1.ChannelRequest) error {
		sending.Lock()
		defer sending.Unlock()
		frame.Binding = binding
		return stream.Send(frame)
	}
	if err := send(&agentv1.ChannelRequest{Body: &agentv1.ChannelRequest_Hello{
		Hello: &agentv1.Hello{Protocol: Protocol, AgentVersion: options.Version},
	}}); err != nil {
		// A send reports only that the stream is gone; the receive side
		// says why, such as a console that failed the handshake.
		if _, cause := stream.Receive(); cause != nil {
			return cause
		}
		return err
	}
	inFlight := newInFlight()
	defer inFlight.wait()
	defer cancel()
	for {
		frame, err := stream.Receive()
		switch {
		case errors.Is(err, io.EOF):
			return errors.New("the console closed the channel")
		case err != nil:
			return err
		}
		if !proto.Equal(frame.GetBinding(), binding) {
			return fmt.Errorf("the console sent a frame for connection %s generation %d",
				frame.GetBinding().GetConnectionUid(), frame.GetBinding().GetConnectionGeneration())
		}
		switch body := frame.GetBody().(type) {
		case *agentv1.ChannelResponse_Request:
			inFlight.start(ctx, body.Request.GetEnvelope().GetRequestId(), func(ctx context.Context) error {
				return dispatcher.Serve(ctx, body.Request, func(result *agentv1.Result) error {
					return send(&agentv1.ChannelRequest{Body: &agentv1.ChannelRequest_Result{Result: result}})
				})
			}, cancel)
		case *agentv1.ChannelResponse_Cancel:
			inFlight.cancel(body.Cancel.GetRequestId())
		default:
			return errors.New("the console sent a frame with no body")
		}
	}
}

// inFlight is the requests a channel is serving, each cancellable by ID.
type inFlight struct {
	group sync.WaitGroup

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func newInFlight() *inFlight {
	return &inFlight{cancels: make(map[string]context.CancelFunc)}
}

// start serves one request. A send that fails ends the channel through
// closeChannel: the stream is gone.
func (f *inFlight) start(ctx context.Context, id string, serve func(context.Context) error, closeChannel context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	// A repeated ID is refused by the dispatcher; it does not take over the
	// cancellation of the request that has the ID.
	_, taken := f.cancels[id]
	owns := id != "" && !taken
	if owns {
		f.cancels[id] = cancel
	}
	f.mu.Unlock()
	f.group.Go(func() {
		defer func() {
			cancel()
			if owns {
				f.mu.Lock()
				delete(f.cancels, id)
				f.mu.Unlock()
			}
		}()
		if err := serve(ctx); err != nil {
			closeChannel()
		}
	})
}

func (f *inFlight) cancel(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cancel, ok := f.cancels[id]; ok {
		cancel()
	}
}

func (f *inFlight) wait() { f.group.Wait() }
