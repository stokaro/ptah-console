package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
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
	// Validity is how long an activated connection's channel credential is
	// accepted from the enrollment's completion.
	Validity time.Duration
	// Activated is told the connection UID of each activated enrollment, so
	// the agent's pending channel ends and opens again as the connection's.
	Activated func(connectionUID string)
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

// ListEnrollments lists the enrollments the person may read.
func (s *ConnectionService) ListEnrollments(ctx context.Context, _ *consolev1.ListEnrollmentsRequest) (*consolev1.ListEnrollmentsResponse, error) {
	user, _, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	list := &consolev1alpha1.ConsoleClusterEnrollmentList{}
	if err := user.List(ctx, list, client.InNamespace(s.self.Namespace)); err != nil {
		return nil, readError(err, "the enrollments")
	}
	response := &consolev1.ListEnrollmentsResponse{}
	for _, item := range list.Items {
		listed := &consolev1.Enrollment{
			ClusterName: item.Spec.ClusterName, ConnectionUid: item.Spec.ConnectionUID, Namespaces: item.Spec.Namespaces,
			ApproverClusterRole: item.Spec.ApproverClusterRole, Fingerprint: item.Status.Fingerprint,
			AgentConnected: meta.IsStatusConditionTrue(item.Status.Conditions, consolev1alpha1.ConditionAgentConnected),
		}
		for _, account := range item.Spec.Accounts {
			roles := make([]string, 0, len(account.Roles))
			for _, role := range account.Roles {
				roles = append(roles, string(role))
			}
			listed.Accounts = append(listed.Accounts, &consolev1.EnrollmentAccount{Id: account.ID, Roles: roles})
		}
		if item.Status.ExpiresAt != nil {
			listed.ExpiresAt = timestamppb.New(item.Status.ExpiresAt.Time)
		}
		if completion := item.Status.Completion; completion != nil {
			listed.Completion = &consolev1.EnrollmentCompletion{
				TargetServer: completion.TargetServer, TargetCaSha256: completion.TargetCASHA256,
				CompletedAt: timestamppb.New(completion.CompletedAt.Time),
			}
		}
		response.Enrollments = append(response.Enrollments, listed)
	}
	return response, nil
}

// ActivateEnrollment makes a completed enrollment whose agent has connected
// a connection, as the person.
func (s *ConnectionService) ActivateEnrollment(ctx context.Context, request *consolev1.ActivateEnrollmentRequest) (*consolev1.ActivateEnrollmentResponse, error) {
	user, principal, err := actor(ctx, s.factory)
	if err != nil {
		return nil, err
	}
	entry, err := enrollment.Activate(ctx, user, s.housekeeping, s.self.Namespace, request.GetClusterName(), s.now(), s.Validity)
	event := audit.Event{
		Action: "activate-enrollment", Context: principal.Context, AccountID: principal.AccountID,
		Namespace: s.self.Namespace, Kind: "ConsoleCluster", Name: request.GetClusterName(), Outcome: audit.Admitted,
	}
	if err != nil {
		event.Outcome = audit.Refused
	} else {
		event.UID = entry.Spec.ConnectionUID
	}
	s.audit.Record(ctx, event)
	if err != nil {
		return nil, activationError(err)
	}
	if s.Activated != nil {
		s.Activated(entry.Spec.ConnectionUID)
	}
	return &consolev1.ActivateEnrollmentResponse{ConnectionUid: entry.Spec.ConnectionUID}, nil
}

// activationError is what the person is told.
func activationError(err error) error {
	switch {
	case errors.Is(err, enrollment.ErrNotCompleted), errors.Is(err, enrollment.ErrAgentNotConnected):
		return connect.NewError(connect.CodeFailedPrecondition, err.Error())
	case errors.Is(err, enrollment.ErrExpired):
		return connect.NewError(connect.CodeDeadlineExceeded, err.Error())
	case apierrors.IsNotFound(err):
		return connect.NewError(connect.CodeNotFound, "no enrollment with that name is visible to you").WithCause(err)
	default:
		return enrollmentError(err)
	}
}
