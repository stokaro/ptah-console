package agentlink

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/enrollment"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	"github.com/stokaro/ptah-console/internal/wire"
)

// pendingEnrollment is the completed enrollment, still within its
// activation window, whose credential the agent presented.
func (h *Hub) pendingEnrollment(ctx context.Context, presented string, now time.Time) (*consolev1alpha1.ConsoleClusterEnrollment, bool, error) {
	enrollments := &consolev1alpha1.ConsoleClusterEnrollmentList{}
	if err := h.housekeeping.List(ctx, enrollments, client.InNamespace(h.namespace)); err != nil {
		return nil, false, err
	}
	verifier := []byte(Verifier(presented))
	var found *consolev1alpha1.ConsoleClusterEnrollment
	for index := range enrollments.Items {
		completion := enrollments.Items[index].Status.Completion
		if completion == nil || !now.Before(completion.CompletedAt.Add(enrollment.ActivationWindow)) {
			continue
		}
		if subtle.ConstantTimeCompare(verifier, []byte(completion.CredentialVerifier)) == 1 {
			found = &enrollments.Items[index]
		}
	}
	return found, found != nil, nil
}

// servePending holds the channel of an enrollment waiting for activation:
// the console records that the agent connected, and sends it nothing. The
// channel ends when the enrollment is activated, so the agent opens it again
// as the connection's.
func (h *Hub) servePending(ctx context.Context, stream agentv1connect.AgentServiceChannelServerStream, pending *consolev1alpha1.ConsoleClusterEnrollment) error {
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	if first.GetHello().GetProtocol() != wire.Protocol {
		return connect.Errorf(connect.CodeFailedPrecondition, "the agent has to open with a hello for protocol %d", wire.Protocol)
	}
	binding := &agentv1.Binding{ConnectionUid: pending.Spec.ConnectionUID, ConnectionGeneration: 1}
	if !proto.Equal(first.GetBinding(), binding) {
		return connect.Errorf(connect.CodePermissionDenied, "the agent bound its channel to connection %s generation %d; its credential is for %s generation 1",
			first.GetBinding().GetConnectionUid(), first.GetBinding().GetConnectionGeneration(), binding.GetConnectionUid())
	}
	key := client.ObjectKeyFromObject(pending)
	if err := h.agentConnected(ctx, key, metav1.ConditionTrue); err != nil {
		return connect.NewError(connect.CodeUnavailable, "the console could not record the agent").WithCause(err)
	}
	ctx, end := context.WithDeadline(ctx, pending.Status.Completion.CompletedAt.Add(enrollment.ActivationWindow))
	defer end()
	h.mu.Lock()
	if older, ok := h.pending[binding.GetConnectionUid()]; ok {
		older()
	}
	h.pending[binding.GetConnectionUid()] = end
	h.mu.Unlock()
	h.logger.Info("the agent of a pending enrollment connected", "connection", binding.GetConnectionUid(), "cluster", pending.Spec.ClusterName)
	defer func() {
		h.mu.Lock()
		delete(h.pending, binding.GetConnectionUid())
		h.mu.Unlock()
		recording, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = h.agentConnected(recording, key, metav1.ConditionFalse)
	}()

	received := make(chan error, 1)
	go func() {
		for {
			if _, err := stream.Receive(); err != nil {
				received <- err
				return
			}
		}
	}()
	select {
	case err := <-received:
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

// agentConnected records whether a pending enrollment's agent is connected.
// An enrollment already gone, as one just activated, is left alone.
func (h *Hub) agentConnected(ctx context.Context, key client.ObjectKey, status metav1.ConditionStatus) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pending := &consolev1alpha1.ConsoleClusterEnrollment{}
		if err := h.housekeeping.Get(ctx, key, pending); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		reason, message := "ChannelOpen", "the agent's channel is open; nothing is sent until the connection is activated"
		if status == metav1.ConditionFalse {
			reason, message = "ChannelClosed", "the agent's channel is closed"
		}
		meta.SetStatusCondition(&pending.Status.Conditions, metav1.Condition{
			Type: consolev1alpha1.ConditionAgentConnected, Status: status, Reason: reason, Message: message,
			ObservedGeneration: pending.Generation,
		})
		return h.housekeeping.Status().Update(ctx, pending)
	})
}

// EndPending ends the channel of a pending enrollment, as its activation
// does, so the agent opens it again as the connection's.
func (h *Hub) EndPending(connectionUID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if end, ok := h.pending[connectionUID]; ok {
		end()
	}
}
