package agentlink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/agent"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
)

// Errors a caller tells apart.
var (
	// ErrNotConnected is a connection with no channel open.
	ErrNotConnected = errors.New("agentlink: the cluster's agent is not connected")
	// ErrLost is a request whose channel ended before its last result. A
	// decision that ends so is unknown: it may or may not have been made.
	ErrLost = errors.New("agentlink: the channel ended before the answer arrived")
)

// results is how many results a request may have waiting: a watch window's
// events, which the caller reads as they come.
const results = 64

// Hub serves the channels agents open and sends requests down them. It
// keeps one channel per connection: a newer one ends the older.
type Hub struct {
	agentv1connect.UnimplementedAgentServiceHandler

	housekeeping client.Client
	namespace    string
	now          func() time.Time
	logger       *slog.Logger

	mu       sync.Mutex
	channels map[string]*channel
}

// NewHub returns a hub for the registry in namespace, which it reads and
// writes the status of through housekeeping, the console's own identity.
func NewHub(housekeeping client.Client, namespace string, now func() time.Time, logger *slog.Logger) *Hub {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Hub{housekeeping: housekeeping, namespace: namespace, now: now, logger: logger, channels: make(map[string]*channel)}
}

// channel is one open channel.
type channel struct {
	binding *agentv1.Binding
	key     client.ObjectKey
	end     context.CancelFunc

	sending sync.Mutex
	stream  agentv1connect.AgentServiceChannelServerStream

	mu      sync.Mutex
	ended   bool
	waiting map[string]*waiter
}

// waiter is a request waiting for its results.
type waiter struct {
	results chan *agentv1.Result
	// stop forgets the request's cancellation once it has ended.
	stop func() bool
}

// finish ends a request's results. The caller holds the channel's lock.
func (c *channel) finish(id string) {
	if waiting, ok := c.waiting[id]; ok {
		close(waiting.results)
		if waiting.stop != nil {
			waiting.stop()
		}
		delete(c.waiting, id)
	}
}

func (c *channel) send(frame *agentv1.ChannelResponse) error {
	c.sending.Lock()
	defer c.sending.Unlock()
	frame.Binding = c.binding
	return c.stream.Send(frame)
}

// Channel serves one agent's channel until it ends, the agent's credential
// stops being accepted, or a newer channel of the same connection opens.
func (h *Hub) Channel(ctx context.Context, stream agentv1connect.AgentServiceChannelServerStream) error {
	info, _ := connect.CallInfoForServerContext(ctx)
	presented, ok := strings.CutPrefix(info.RequestHeader().Get("Authorization"), "Bearer ")
	if !ok || presented == "" {
		return connect.NewError(connect.CodeUnauthenticated, "the channel needs a credential")
	}
	now := h.now()
	entry, credential, err := h.authenticate(ctx, presented, now)
	if err != nil {
		return err
	}
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || hello.GetProtocol() != agent.Protocol {
		return connect.Errorf(connect.CodeFailedPrecondition, "the agent has to open with a hello for protocol %d", agent.Protocol)
	}
	binding := &agentv1.Binding{ConnectionUid: entry.Spec.ConnectionUID, ConnectionGeneration: uint64(entry.Status.Channel.Generation)} // #nosec G115 -- the schema keeps it at 1 or more.
	if !proto.Equal(first.GetBinding(), binding) {
		return connect.Errorf(connect.CodePermissionDenied, "the agent bound its channel to connection %s generation %d; its credential is for %s generation %d",
			first.GetBinding().GetConnectionUid(), first.GetBinding().GetConnectionGeneration(), binding.GetConnectionUid(), binding.GetConnectionGeneration())
	}
	key := client.ObjectKeyFromObject(entry)
	if err := h.markUsed(ctx, key, credential.Verifier, now); err != nil {
		return connect.NewError(connect.CodeUnavailable, "the console could not record the channel").WithCause(err)
	}

	ctx, end := context.WithDeadline(ctx, endOf(credential))
	defer end()
	open := &channel{binding: binding, key: key, end: end, stream: stream, waiting: make(map[string]*waiter)}
	h.replace(open)
	defer h.closed(open)
	h.logger.Info("an agent opened its channel", "connection", binding.GetConnectionUid(), "generation", binding.GetConnectionGeneration(),
		"agent", hello.GetAgentVersion())

	received := make(chan error, 1)
	go func() { received <- h.receive(open, stream) }()
	select {
	case err := <-received:
		return err
	case <-ctx.Done():
		return nil
	}
}

// authenticate finds the Agent entry whose live credential the agent
// presented. Every refusal reads the same, so a caller learns nothing about
// which credentials exist.
func (h *Hub) authenticate(ctx context.Context, presented string, now time.Time) (*consolev1alpha1.ConsoleCluster, *consolev1alpha1.ChannelCredential, error) {
	entries := &consolev1alpha1.ConsoleClusterList{}
	if err := h.housekeeping.List(ctx, entries, client.InNamespace(h.namespace)); err != nil {
		return nil, nil, connect.NewError(connect.CodeUnavailable, "the console could not read its registry").WithCause(err)
	}
	var found *consolev1alpha1.ConsoleCluster
	var credential *consolev1alpha1.ChannelCredential
	matches := 0
	for index := range entries.Items {
		entry := &entries.Items[index]
		if entry.Spec.Profile != consolev1alpha1.ProfileAgent {
			continue
		}
		if match, ok := accepted(entry, presented, now); ok {
			found, credential = entry, match
			matches++
		}
	}
	if matches != 1 {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, "the credential is not one this console accepts")
	}
	return found, credential, nil
}

// markUsed records that the credential authenticated a channel, and retires
// an older credential of the connection: it stays accepted for at most
// RotationOverlap.
func (h *Hub) markUsed(ctx context.Context, key client.ObjectKey, verifier string, now time.Time) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry := &consolev1alpha1.ConsoleCluster{}
		if err := h.housekeeping.Get(ctx, key, entry); err != nil {
			return err
		}
		credentials := entry.Status.Channel.Credentials
		var used *consolev1alpha1.ChannelCredential
		for index := range credentials {
			if credentials[index].Verifier == verifier {
				used = &credentials[index]
			}
		}
		if used == nil {
			return errors.New("the credential was removed while the channel opened")
		}
		if used.FirstUsedAt == nil {
			used.FirstUsedAt = &metav1.Time{Time: now}
		}
		for index := range credentials {
			older := &credentials[index]
			if older.Verifier == verifier || !older.IssuedAt.Before(&used.IssuedAt) || older.RetireAt != nil {
				continue
			}
			retire := metav1.NewTime(now.Add(RotationOverlap))
			if older.NotAfter.Before(&retire) {
				retire = older.NotAfter
			}
			older.RetireAt = &retire
		}
		entry.Status.LastObservedAt = &metav1.Time{Time: now}
		meta.SetStatusCondition(&entry.Status.Conditions, metav1.Condition{
			Type: consolev1alpha1.ConditionConnected, Status: metav1.ConditionTrue, Reason: "ChannelOpen",
			Message: "the agent's channel is open", ObservedGeneration: entry.Generation,
		})
		return h.housekeeping.Status().Update(ctx, entry)
	})
}

// replace makes open the connection's channel and ends the one it replaces.
func (h *Hub) replace(open *channel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.channels[open.binding.GetConnectionUid()]; ok {
		old.end()
	}
	h.channels[open.binding.GetConnectionUid()] = open
}

// closed forgets a channel that ended, and fails what it was still waiting
// for.
func (h *Hub) closed(open *channel) {
	h.mu.Lock()
	current := h.channels[open.binding.GetConnectionUid()] == open
	if current {
		delete(h.channels, open.binding.GetConnectionUid())
	}
	h.mu.Unlock()
	open.mu.Lock()
	open.ended = true
	for id := range open.waiting {
		open.finish(id)
	}
	open.mu.Unlock()
	if current {
		h.disconnected(open.key)
	}
}

// disconnected records that the connection has no channel.
func (h *Hub) disconnected(key client.ObjectKey) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry := &consolev1alpha1.ConsoleCluster{}
		if err := h.housekeeping.Get(ctx, key, entry); err != nil {
			return client.IgnoreNotFound(err)
		}
		meta.SetStatusCondition(&entry.Status.Conditions, metav1.Condition{
			Type: consolev1alpha1.ConditionConnected, Status: metav1.ConditionFalse, Reason: "ChannelClosed",
			Message: "the agent's channel is closed", ObservedGeneration: entry.Generation,
		})
		return h.housekeeping.Status().Update(ctx, entry)
	})
	if err != nil {
		h.logger.Warn("the console could not record a closed channel", "connection", key, "error", err)
	}
}

// receive hands the agent's results to the requests that wait for them. A
// frame bound to anything else, or a second hello, ends the channel.
func (h *Hub) receive(open *channel, stream agentv1connect.AgentServiceChannelServerStream) error {
	for {
		frame, err := stream.Receive()
		if err != nil {
			return err
		}
		if !proto.Equal(frame.GetBinding(), open.binding) {
			return connect.NewError(connect.CodePermissionDenied, "the agent sent a frame for another connection or generation")
		}
		result := frame.GetResult()
		if result == nil {
			return connect.NewError(connect.CodeInvalidArgument, "the agent sent a frame with no result")
		}
		open.deliver(result)
	}
}

// deliver passes a result to its request, and ends the request with its
// last result. A result for a request nobody waits for is dropped.
func (c *channel) deliver(result *agentv1.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := result.GetRequestId()
	waiting, ok := c.waiting[id]
	if !ok {
		return
	}
	select {
	case waiting.results <- result:
		if result.GetLast() {
			c.finish(id)
		}
	default:
		// The caller stopped reading; the request is over for it.
		c.finish(id)
	}
}

// Pending is a request sent down a channel.
type Pending struct {
	// ID is the request ID the console gave it.
	ID string
	// Results are the agent's results. The channel closes after the last
	// one, or without it when the channel to the agent ended first.
	Results <-chan *agentv1.Result
}

// Send sends a request down the connection's channel with a fresh request
// ID. The envelope's other fields are the caller's. Cancelling ctx asks the
// agent to end the request.
func (h *Hub) Send(ctx context.Context, connectionUID string, request *agentv1.Request) (*Pending, error) {
	h.mu.Lock()
	open, ok := h.channels[connectionUID]
	h.mu.Unlock()
	if !ok {
		return nil, ErrNotConnected
	}
	id, err := newRequestID()
	if err != nil {
		return nil, err
	}
	request = proto.CloneOf(request)
	if request.Envelope == nil {
		request.Envelope = &agentv1.Envelope{}
	}
	request.Envelope.RequestId = id
	waiting := &waiter{results: make(chan *agentv1.Result, results)}
	open.mu.Lock()
	if open.ended {
		open.mu.Unlock()
		return nil, ErrNotConnected
	}
	open.waiting[id] = waiting
	open.mu.Unlock()
	if err := open.send(&agentv1.ChannelResponse{Body: &agentv1.ChannelResponse_Request{Request: request}}); err != nil {
		open.end()
		return nil, fmt.Errorf("%w: %w", ErrLost, err)
	}
	stop := context.AfterFunc(ctx, func() {
		open.mu.Lock()
		_, pending := open.waiting[id]
		open.mu.Unlock()
		if pending {
			_ = open.send(&agentv1.ChannelResponse{Body: &agentv1.ChannelResponse_Cancel{Cancel: &agentv1.Cancel{RequestId: id}}})
		}
	})
	open.mu.Lock()
	if _, pending := open.waiting[id]; pending {
		waiting.stop = stop
	} else {
		stop()
	}
	open.mu.Unlock()
	return &Pending{ID: id, Results: waiting.results}, nil
}

// End ends the connection's channel, if one is open.
func (h *Hub) End(connectionUID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if open, ok := h.channels[connectionUID]; ok {
		open.end()
	}
}

// Connected reports whether the connection has a channel open.
func (h *Hub) Connected(connectionUID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.channels[connectionUID]
	return ok
}

func newRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
