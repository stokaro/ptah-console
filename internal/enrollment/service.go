package enrollment

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/audit"
	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
)

// Service serves EnrollmentService on the console's agent listener.
type Service struct {
	agentv1connect.UnimplementedEnrollmentServiceHandler

	housekeeping client.Client
	namespace    string
	now          func() time.Time
	audit        *audit.Logger
}

// NewService returns the service for the registry in namespace.
func NewService(housekeeping client.Client, namespace string, now func() time.Time, auditLog *audit.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if auditLog == nil {
		auditLog = audit.Discard()
	}
	return &Service{housekeeping: housekeeping, namespace: namespace, now: now, audit: auditLog}
}

// CompleteEnrollment records the helper's report.
func (s *Service) CompleteEnrollment(ctx context.Context, request *agentv1.CompleteEnrollmentRequest) (*agentv1.CompleteEnrollmentResponse, error) {
	completed, err := Complete(ctx, s.housekeeping, s.namespace, Completion{
		ConnectionUID: request.GetConnectionUid(), Bootstrap: request.GetBootstrap(), Fingerprint: request.GetFingerprint(),
		TargetServer: request.GetTargetServer(), TargetCASHA256: request.GetTargetCaSha256(),
		CredentialVerifier: request.GetCredentialVerifier(),
	}, s.now())
	event := audit.Event{
		Action: "complete-enrollment", Namespace: s.namespace, Kind: "ConsoleClusterEnrollment",
		UID: request.GetConnectionUid(), Outcome: audit.Admitted,
	}
	if completed != nil {
		event.Name = completed.Spec.ClusterName
	}
	if err != nil {
		event.Outcome, event.Reason = audit.Refused, "the report was not accepted"
	}
	s.audit.Record(ctx, event)
	switch {
	case err == nil:
		return &agentv1.CompleteEnrollmentResponse{ClusterName: completed.Spec.ClusterName}, nil
	case errors.Is(err, ErrNotAccepted):
		return nil, connect.NewError(connect.CodeUnauthenticated, err.Error())
	case errors.Is(err, ErrNotThisFile):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err.Error())
	case errors.Is(err, ErrCompletedOtherwise):
		return nil, connect.NewError(connect.CodeAlreadyExists, err.Error())
	case apierrors.IsInvalid(err):
		return nil, connect.NewError(connect.CodeInvalidArgument, "the report names a target the registry does not accept").WithCause(err)
	default:
		return nil, connect.NewError(connect.CodeUnavailable, "the console could not record the enrollment").WithCause(err)
	}
}
