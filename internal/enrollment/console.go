package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

// Errors a caller tells apart.
var (
	// ErrNameTaken is a cluster name an entry or a pending enrollment has.
	ErrNameTaken = errors.New("enrollment: a connection or a pending enrollment already has that name")
	// ErrTooMany is a console that holds MaxPending enrollments.
	ErrTooMany = fmt.Errorf("enrollment: the console holds %d pending enrollments; finish or let some expire first", MaxPending)
	// ErrUnknownAccount is an account the console does not have.
	ErrUnknownAccount = errors.New("enrollment: no Console account has that ID")
	// ErrNotAccepted is a completion with an unknown, expired or wrong
	// bootstrap secret. Every such refusal reads the same.
	ErrNotAccepted = errors.New("enrollment: the enrollment is unknown, expired or not this one; start a new one in the console")
	// ErrNotThisFile is a completion whose fingerprint is not the one the
	// console issued.
	ErrNotThisFile = errors.New("enrollment: the file does not match what the console issued")
	// ErrCompletedOtherwise is a completion of an enrollment already completed
	// with another target or credential.
	ErrCompletedOtherwise = errors.New("enrollment: the enrollment was already completed with another target or credential; start a new one")
)

// Self is what the console knows of itself for an enrollment.
type Self struct {
	// Namespace is the registry's.
	Namespace      string
	InstallationID string
	// Accounts are the console's account IDs.
	Accounts []string
	// Agents is the console's agent listener and CA.
	Agents Console
	Chart  Chart
}

// Request is what an installation administrator asks for.
type Request struct {
	ClusterName         string
	Namespaces          []string
	Accounts            []Account
	ApproverClusterRole string
}

// Create starts an enrollment. The administrator's own client creates it, so
// RBAC decides who may; the console's own identity then records the
// bootstrap verifier, the expiry and the fingerprint. The file it returns
// holds the bootstrap secret, which nothing keeps.
func Create(ctx context.Context, user, housekeeping client.Client, self Self, request Request, now time.Time) (*File, error) {
	for _, account := range request.Accounts {
		if !slices.Contains(self.Accounts, account.ID) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownAccount, account.ID)
		}
	}
	pending := &consolev1alpha1.ConsoleClusterEnrollmentList{}
	if err := housekeeping.List(ctx, pending, client.InNamespace(self.Namespace)); err != nil {
		return nil, fmt.Errorf("list the pending enrollments: %w", err)
	}
	if len(pending.Items) >= MaxPending {
		return nil, ErrTooMany
	}
	err := housekeeping.Get(ctx, client.ObjectKey{Namespace: self.Namespace, Name: request.ClusterName}, &consolev1alpha1.ConsoleCluster{})
	switch {
	case err == nil:
		return nil, ErrNameTaken
	case !apierrors.IsNotFound(err):
		return nil, fmt.Errorf("read the registry: %w", err)
	}
	connection, err := newUUID()
	if err != nil {
		return nil, err
	}
	enrolled := make([]consolev1alpha1.EnrollmentAccount, 0, len(request.Accounts))
	for _, account := range request.Accounts {
		roles := make([]consolev1alpha1.EnrollmentRole, 0, len(account.Roles))
		for _, role := range account.Roles {
			roles = append(roles, consolev1alpha1.EnrollmentRole(role))
		}
		enrolled = append(enrolled, consolev1alpha1.EnrollmentAccount{ID: account.ID, Roles: roles})
	}
	enrollment := &consolev1alpha1.ConsoleClusterEnrollment{
		ObjectMeta: metav1.ObjectMeta{Namespace: self.Namespace, Name: request.ClusterName},
		Spec: consolev1alpha1.ConsoleClusterEnrollmentSpec{
			ClusterName: request.ClusterName, ConnectionUID: connection, InstallationID: self.InstallationID,
			Namespaces: slices.Clone(request.Namespaces), Accounts: enrolled, ApproverClusterRole: request.ApproverClusterRole,
			ConsoleServerName: self.Agents.ServerName, ConsoleCASHA256: CASHA256(self.Agents.CABundle),
		},
	}
	if err := user.Create(ctx, enrollment); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	bootstrap, err := newSecret()
	if err != nil {
		return nil, err
	}
	identity := IdentityOf(enrollment.Spec)
	expiresAt := now.Add(BootstrapValidity)
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &consolev1alpha1.ConsoleClusterEnrollment{}
		if err := housekeeping.Get(ctx, client.ObjectKeyFromObject(enrollment), current); err != nil {
			return err
		}
		if current.UID != enrollment.UID {
			return errors.New("the enrollment was replaced while it was being issued")
		}
		current.Status.BootstrapVerifier = Verifier(bootstrap)
		current.Status.ExpiresAt = &metav1.Time{Time: expiresAt}
		current.Status.Fingerprint = identity.Fingerprint()
		return housekeeping.Status().Update(ctx, current)
	})
	if err != nil {
		return nil, fmt.Errorf("record the enrollment's bootstrap: %w", err)
	}
	return &File{
		APIVersion: APIVersion, Kind: Kind, Console: self.Agents, Identity: identity, Chart: self.Chart,
		Bootstrap: bootstrap, ExpiresAt: expiresAt.UTC().Truncate(time.Second), Fingerprint: identity.Fingerprint(),
	}, nil
}

// Completion is what the target administrator's helper reports.
type Completion struct {
	ConnectionUID      string
	Bootstrap          string
	Fingerprint        string
	TargetServer       string
	TargetCASHA256     string
	CredentialVerifier string
}

// Complete records the helper's report against the enrollment its bootstrap
// secret belongs to. The secret completes one enrollment once; the same
// report sent again is accepted again, so a helper that lost the answer can
// retry, and another report is refused.
func Complete(ctx context.Context, housekeeping client.Client, namespace string, report Completion, now time.Time) (*consolev1alpha1.ConsoleClusterEnrollment, error) {
	var completed *consolev1alpha1.ConsoleClusterEnrollment
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		enrollment, err := find(ctx, housekeeping, namespace, report.ConnectionUID)
		if err != nil {
			return err
		}
		status := &enrollment.Status
		if status.BootstrapVerifier == "" ||
			subtle.ConstantTimeCompare([]byte(Verifier(report.Bootstrap)), []byte(status.BootstrapVerifier)) != 1 {
			return ErrNotAccepted
		}
		if report.Fingerprint != status.Fingerprint {
			return ErrNotThisFile
		}
		if status.Completion != nil {
			if status.Completion.TargetServer != report.TargetServer || status.Completion.TargetCASHA256 != report.TargetCASHA256 ||
				status.Completion.CredentialVerifier != report.CredentialVerifier {
				return ErrCompletedOtherwise
			}
			completed = enrollment
			return nil
		}
		if status.ExpiresAt == nil || !now.Before(status.ExpiresAt.Time) {
			return ErrNotAccepted
		}
		status.Completion = &consolev1alpha1.EnrollmentCompletion{
			TargetServer: report.TargetServer, TargetCASHA256: report.TargetCASHA256,
			CredentialVerifier: report.CredentialVerifier, CompletedAt: metav1.Time{Time: now},
		}
		if err := housekeeping.Status().Update(ctx, enrollment); err != nil {
			return err
		}
		completed = enrollment
		return nil
	})
	return completed, err
}

// find is the enrollment of a connection UID.
func find(ctx context.Context, housekeeping client.Reader, namespace, connectionUID string) (*consolev1alpha1.ConsoleClusterEnrollment, error) {
	enrollments := &consolev1alpha1.ConsoleClusterEnrollmentList{}
	if err := housekeeping.List(ctx, enrollments, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list the pending enrollments: %w", err)
	}
	for index := range enrollments.Items {
		if enrollments.Items[index].Spec.ConnectionUID == connectionUID {
			return &enrollments.Items[index], nil
		}
	}
	return nil, ErrNotAccepted
}

// expired reports whether an enrollment is past its time: its bootstrap
// unused past its expiry, or its completion unactivated past the activation
// window.
func expired(enrollment *consolev1alpha1.ConsoleClusterEnrollment, now time.Time) bool {
	status := enrollment.Status
	switch {
	case status.Completion != nil:
		return !now.Before(status.Completion.CompletedAt.Add(ActivationWindow))
	case status.ExpiresAt != nil:
		return !now.Before(status.ExpiresAt.Time)
	default:
		// Created, and the console never recorded its bootstrap: it can
		// never complete.
		return !now.Before(enrollment.CreationTimestamp.Add(BootstrapValidity))
	}
}

// Tend deletes the enrollments past their time.
func Tend(ctx context.Context, housekeeping client.Client, namespace string, now time.Time) error {
	enrollments := &consolev1alpha1.ConsoleClusterEnrollmentList{}
	if err := housekeeping.List(ctx, enrollments, client.InNamespace(namespace)); err != nil {
		return err
	}
	var errs []error
	for index := range enrollments.Items {
		enrollment := &enrollments.Items[index]
		if !expired(enrollment, now) {
			continue
		}
		err := housekeeping.Delete(ctx, enrollment, client.Preconditions{UID: &enrollment.UID, ResourceVersion: &enrollment.ResourceVersion})
		if client.IgnoreNotFound(err) != nil && !apierrors.IsConflict(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// TendAll tends the enrollments every interval until ctx ends.
func TendAll(ctx context.Context, housekeeping client.Client, namespace string, interval time.Duration, now func() time.Time, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := Tend(ctx, housekeeping, namespace, now()); err != nil && ctx.Err() == nil {
			logger.Warn("the console could not tend its pending enrollments", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// newSecret is 256 random bits.
func newSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// newUUID is a random version 4 UUID.
func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// Errors of activation.
var (
	// ErrNotCompleted is an enrollment the target's helper has not completed.
	ErrNotCompleted = errors.New("enrollment: the target's helper has not completed the enrollment yet")
	// ErrAgentNotConnected is an enrollment whose agent has not opened its
	// channel: nothing shows the agent runs with the credential.
	ErrAgentNotConnected = errors.New("enrollment: the agent has not connected; install it and wait until the console shows it connected")
	// ErrExpired is an enrollment past its time.
	ErrExpired = errors.New("enrollment: the enrollment expired; start a new one")
)

// Activate makes a completed enrollment whose agent has connected a
// connection. The administrator's own client creates the registry entry, so
// RBAC decides who may; the console's identity records the channel
// credential's verifier, valid for validity from the completion, and removes
// the enrollment.
func Activate(ctx context.Context, user, housekeeping client.Client, namespace, name string, now time.Time, validity time.Duration) (*consolev1alpha1.ConsoleCluster, error) {
	pending := &consolev1alpha1.ConsoleClusterEnrollment{}
	if err := user.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pending); err != nil {
		return nil, err
	}
	completion := pending.Status.Completion
	switch {
	case expired(pending, now):
		return nil, ErrExpired
	case completion == nil:
		return nil, ErrNotCompleted
	case !meta.IsStatusConditionTrue(pending.Status.Conditions, consolev1alpha1.ConditionAgentConnected):
		return nil, ErrAgentNotConnected
	}
	entry := &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: pending.Spec.ClusterName},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: pending.Spec.ConnectionUID, Profile: consolev1alpha1.ProfileAgent,
			InstallationID: pending.Spec.InstallationID, Namespaces: slices.Clone(pending.Spec.Namespaces),
			ConfigurationOwner: consolev1alpha1.OwnerConsole,
			Agent: &consolev1alpha1.AgentTrust{
				TargetServer: completion.TargetServer, TargetCASHA256: completion.TargetCASHA256,
				ConsoleServerName: pending.Spec.ConsoleServerName, ConsoleCASHA256: pending.Spec.ConsoleCASHA256,
			},
		},
	}
	if err := user.Create(ctx, entry); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		// An activation that stopped part way left this enrollment's entry;
		// any other entry of that name is another connection's.
		existing := &consolev1alpha1.ConsoleCluster{}
		if err := user.Get(ctx, client.ObjectKeyFromObject(entry), existing); err != nil {
			return nil, err
		}
		if existing.Spec.ConnectionUID != pending.Spec.ConnectionUID {
			return nil, ErrNameTaken
		}
		entry = existing
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &consolev1alpha1.ConsoleCluster{}
		if err := housekeeping.Get(ctx, client.ObjectKeyFromObject(entry), current); err != nil {
			return err
		}
		if current.Status.Channel != nil {
			return nil
		}
		current.Status.Channel = &consolev1alpha1.ChannelStatus{Generation: 1, Credentials: []consolev1alpha1.ChannelCredential{{
			Verifier: completion.CredentialVerifier, IssuedAt: completion.CompletedAt,
			NotAfter: metav1.NewTime(completion.CompletedAt.Add(validity)),
		}}}
		return housekeeping.Status().Update(ctx, current)
	})
	if err != nil {
		return nil, fmt.Errorf("record the connection's channel credential: %w", err)
	}
	if err := grantAccounts(ctx, user, entry, pending.Spec.Accounts); err != nil {
		return nil, fmt.Errorf("let the enrolled accounts see the connection: %w", err)
	}
	err = housekeeping.Delete(ctx, pending, client.Preconditions{UID: &pending.UID})
	if client.IgnoreNotFound(err) != nil {
		return nil, fmt.Errorf("remove the activated enrollment: %w", err)
	}
	return entry, nil
}

// GrantName is the Role and RoleBinding that let a connection's enrolled
// accounts see it. The connection UID is in the name, so a name reused by a
// new connection gets grants of its own, whatever the garbage collector has
// not yet removed of the old one's.
func GrantName(connectionUID string) string {
	return "ptah-console-connection-" + connectionUID
}

// grantAccounts lets the enrolled accounts get the connection's entry, and
// nothing else: the console shows a person only the entries that person may
// get. The administrator's own client makes the grant, so RBAC decides who
// may, and the entry owns it, so it goes with the entry.
func grantAccounts(ctx context.Context, user client.Client, entry *consolev1alpha1.ConsoleCluster, enrolled []consolev1alpha1.EnrollmentAccount) error {
	owner := metav1.OwnerReference{
		APIVersion: consolev1alpha1.GroupVersion.String(), Kind: "ConsoleCluster", Name: entry.Name, UID: entry.UID,
	}
	named := metav1.ObjectMeta{
		Namespace: entry.Namespace, Name: GrantName(entry.Spec.ConnectionUID), OwnerReferences: []metav1.OwnerReference{owner},
	}
	subjects := make([]rbacv1.Subject, 0, len(enrolled))
	for _, account := range enrolled {
		subjects = append(subjects, rbacv1.Subject{
			APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind,
			Name: accounts.UsernamePrefix + entry.Spec.InstallationID + ":" + account.ID,
		})
	}
	for _, object := range []client.Object{
		&rbacv1.Role{ObjectMeta: *named.DeepCopy(), Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{consolev1alpha1.GroupVersion.Group}, Resources: []string{"consoleclusters"},
			ResourceNames: []string{entry.Name}, Verbs: []string{"get"},
		}}},
		&rbacv1.RoleBinding{
			ObjectMeta: *named.DeepCopy(), Subjects: subjects,
			RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: named.Name},
		},
	} {
		if err := user.Create(ctx, object); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}
