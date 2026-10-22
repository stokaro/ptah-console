package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/enrollment"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// ConnectionService serves ConnectionService.
type ConnectionService struct {
	consolev1connect.UnimplementedConnectionServiceHandler

	factory      kube.ClientFactory
	housekeeping client.Client
	self         enrollment.Self
	now          func() time.Time
	audit        *audit.Logger
}

// NewConnectionService returns the ConnectionService of a console that
// serves agents. housekeeping is the console's own identity, which records
// what the person's request created.
func NewConnectionService(factory kube.ClientFactory, housekeeping client.Client, self enrollment.Self, now func() time.Time, auditLog *audit.Logger) *ConnectionService {
	if now == nil {
		now = time.Now
	}
	return &ConnectionService{factory: factory, housekeeping: housekeeping, self: self, now: now, audit: auditLog}
}

// CreateEnrollment creates the enrollment as the person, and answers with its
// file once.
func (s *ConnectionService) CreateEnrollment(ctx context.Context, request *consolev1.CreateEnrollmentRequest) (*consolev1.CreateEnrollmentResponse, error) {
	user, principal, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	accounts := make([]enrollment.Account, 0, len(request.GetAccounts()))
	for _, account := range request.GetAccounts() {
		accounts = append(accounts, enrollment.Account{ID: account.GetId(), Roles: account.GetRoles()})
	}
	file, err := enrollment.Create(ctx, user, s.housekeeping, s.self, enrollment.Request{
		ClusterName: request.GetClusterName(), Namespaces: request.GetNamespaces(), Accounts: accounts,
		ApproverClusterRole: request.GetApproverClusterRole(),
	}, s.now())
	event := audit.Event{
		Action: "create-enrollment", Context: principal.Context, AccountID: principal.AccountID,
		Namespace: s.self.Namespace, Kind: "ConsoleClusterEnrollment", Name: request.GetClusterName(), Outcome: audit.Admitted,
	}
	if file != nil {
		event.UID = file.Identity.ConnectionUID
	}
	if err != nil {
		event.Outcome = audit.Refused
	}
	s.audit.Record(ctx, event)
	if err != nil {
		return nil, enrollmentError(err)
	}
	encoded, err := file.Marshal()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "the enrollment file could not be written").WithCause(err)
	}
	return &consolev1.CreateEnrollmentResponse{
		EnrollmentFile: encoded, Fingerprint: file.Fingerprint, ExpiresAt: timestamppb.New(file.ExpiresAt),
		ConnectionUid: file.Identity.ConnectionUID,
	}, nil
}

// enrollmentError is what the person is told.
func enrollmentError(err error) error {
	var status apierrors.APIStatus
	switch {
	case errors.Is(err, enrollment.ErrNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, err.Error())
	case errors.Is(err, enrollment.ErrTooMany):
		return connect.NewError(connect.CodeResourceExhausted, err.Error())
	case errors.Is(err, enrollment.ErrUnknownAccount):
		return connect.NewError(connect.CodeInvalidArgument, err.Error())
	case errors.As(err, &status) && apierrors.IsForbidden(err):
		return connect.NewError(connect.CodePermissionDenied, status.Status().Message).WithCause(err)
	case apierrors.IsInvalid(err):
		return connect.NewError(connect.CodeInvalidArgument, err.Error()).WithCause(err)
	default:
		return connect.NewError(connect.CodeUnavailable, "the enrollment could not be created").WithCause(err)
	}
}
