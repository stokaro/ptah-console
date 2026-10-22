package agentlink

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
)

// Errors of revocation.
var (
	// ErrRemoved is a connection that was removed: nothing revives it.
	ErrRemoved = errors.New("agentlink: the connection was removed; enroll a new one")
	// ErrNotAllowed is a person who may not change the connection.
	ErrNotAllowed = errors.New("agentlink: you may not change this connection")
)

// Fence is what the revocations say of one connection.
type Fence struct {
	// Removed is set when every generation is fenced.
	Removed bool
	// Through is the highest generation a quarantine fences; zero is none.
	Through int64
}

// Fences is whether generation of the connection is fenced.
func (f Fence) Fences(generation int64) bool {
	return f.Removed || generation <= f.Through
}

// FenceOf reads what the revocations in namespace fence of a connection.
func FenceOf(ctx context.Context, reader client.Reader, namespace, connectionUID string) (Fence, error) {
	revocations := &consolev1alpha1.ConsoleClusterRevocationList{}
	if err := reader.List(ctx, revocations, client.InNamespace(namespace)); err != nil {
		return Fence{}, fmt.Errorf("read the revocations: %w", err)
	}
	var fence Fence
	for _, revocation := range revocations.Items {
		if revocation.Spec.ConnectionUID != connectionUID {
			continue
		}
		switch {
		case revocation.Spec.Action == consolev1alpha1.RevocationRemove:
			fence.Removed = true
		case revocation.Spec.Generation != nil && *revocation.Spec.Generation > fence.Through:
			fence.Through = *revocation.Spec.Generation
		}
	}
	return fence, nil
}

// mayChange asks the API server whether the person may delete the entry,
// which is the authority quarantine and removal stand on: the console's own
// identity writes the fence, so the person's right is checked first.
func mayChange(ctx context.Context, user client.Client, key client.ObjectKey) error {
	review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authorizationv1.ResourceAttributes{
			Group: consolev1alpha1.GroupVersion.Group, Resource: "consoleclusters", Verb: "delete",
			Namespace: key.Namespace, Name: key.Name,
		},
	}}
	if err := user.Create(ctx, review); err != nil {
		return fmt.Errorf("ask whether you may change the connection: %w", err)
	}
	if !review.Status.Allowed {
		return ErrNotAllowed
	}
	return nil
}

// revoke records a fence. The same fence recorded again is the same record.
func revoke(ctx context.Context, housekeeping client.Client, entry *consolev1alpha1.ConsoleCluster, action consolev1alpha1.RevocationAction,
	generation *int64, reason, account string,
) error {
	name := entry.Spec.ConnectionUID + "-removed"
	if generation != nil {
		name = entry.Spec.ConnectionUID + "-g" + strconv.FormatInt(*generation, 10)
	}
	revocation := &consolev1alpha1.ConsoleClusterRevocation{
		ObjectMeta: metav1.ObjectMeta{Namespace: entry.Namespace, Name: name},
		Spec: consolev1alpha1.ConsoleClusterRevocationSpec{
			ConnectionUID: entry.Spec.ConnectionUID, Generation: generation, Action: action,
			ClusterName: entry.Name, Reason: reason, RevokedBy: account,
		},
	}
	if err := housekeeping.Create(ctx, revocation); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("record the revocation: %w", err)
	}
	return nil
}

// Quarantine fences the connection's channel at its generation and drops
// its credentials, as the person, who has to be allowed to delete the entry.
// The fence is kept apart from the entry, so a recreated entry does not lift
// it; new credentials at a higher generation do. It returns the fenced
// generation. The caller ends the open channel.
func Quarantine(ctx context.Context, user, housekeeping client.Client, key client.ObjectKey, reason, account string) (int64, error) {
	if err := mayChange(ctx, user, key); err != nil {
		return 0, err
	}
	entry := &consolev1alpha1.ConsoleCluster{}
	if err := housekeeping.Get(ctx, key, entry); err != nil {
		return 0, err
	}
	if entry.Spec.Profile != consolev1alpha1.ProfileAgent {
		return 0, fmt.Errorf("agentlink: %s is a %s connection; only an agent's channel is quarantined", key, entry.Spec.Profile)
	}
	generation := int64(1)
	if entry.Status.Channel != nil {
		generation = entry.Status.Channel.Generation
	}
	if err := revoke(ctx, housekeeping, entry, consolev1alpha1.RevocationQuarantine, &generation, reason, account); err != nil {
		return 0, err
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &consolev1alpha1.ConsoleCluster{}
		if err := housekeeping.Get(ctx, key, current); err != nil {
			return err
		}
		if current.Status.Channel == nil || len(current.Status.Channel.Credentials) == 0 {
			return nil
		}
		current.Status.Channel.Credentials = nil
		return housekeeping.Status().Update(ctx, current)
	})
	return generation, err
}

// Removal is what removing a connection left to the target's administrator.
type Removal struct {
	ConnectionUID string
	// Cleanup is the target-side steps, which stay pending: the console
	// cannot see the target do them.
	Cleanup []string
}

// Remove revokes the connection first, then removes its preferences and its
// entry as the person, and keeps the revocation as its tombstone. It does
// not reach the target: an offline target is removed all the same, and its
// cleanup is left to its administrator. The caller ends the open channel.
func Remove(ctx context.Context, user, housekeeping client.Client, key client.ObjectKey, reason, account string) (*Removal, error) {
	if err := mayChange(ctx, user, key); err != nil {
		return nil, err
	}
	entry := &consolev1alpha1.ConsoleCluster{}
	if err := housekeeping.Get(ctx, key, entry); err != nil {
		return nil, err
	}
	if entry.Spec.Profile != consolev1alpha1.ProfileAgent {
		return nil, fmt.Errorf("agentlink: %s is the cluster the console runs in; it is not removed here", key)
	}
	if err := revoke(ctx, housekeeping, entry, consolev1alpha1.RevocationRemove, nil, reason, account); err != nil {
		return nil, err
	}
	preferences := &consolev1alpha1.ConsoleClusterPreferencesList{}
	if err := housekeeping.List(ctx, preferences, client.InNamespace(key.Namespace)); err != nil {
		return nil, err
	}
	for index := range preferences.Items {
		if preferences.Items[index].Spec.ClusterRef.ConnectionUID != entry.Spec.ConnectionUID {
			continue
		}
		if err := client.IgnoreNotFound(user.Delete(ctx, &preferences.Items[index])); err != nil {
			return nil, fmt.Errorf("remove the connection's preferences: %w", err)
		}
	}
	if err := client.IgnoreNotFound(user.Delete(ctx, entry, client.Preconditions{UID: &entry.UID})); err != nil {
		return nil, fmt.Errorf("remove the connection: %w", err)
	}
	return &Removal{ConnectionUID: entry.Spec.ConnectionUID, Cleanup: cleanup()}, nil
}

// cleanup is what the target's administrator does after a removal, with the
// names the enrollment helper uses by default. Removing the agent's
// impersonation binding first ends its authority at once.
func cleanup() []string {
	return []string{
		"kubectl delete clusterrolebinding ptah-console-agent-impersonate   # ends the agent's authority at once",
		"helm uninstall ptah-console-agent --namespace ptah-console-agent",
		"kubectl --namespace ptah-console-agent delete secret ptah-console-agent-credential",
	}
}

// IssueAs issues a credential for a person allowed to delete the entry: the
// console's own identity writes the verifier, so the person's right is
// checked first.
func IssueAs(ctx context.Context, user, housekeeping client.Client, key client.ObjectKey, now time.Time, validity time.Duration) (string, error) {
	if err := mayChange(ctx, user, key); err != nil {
		return "", err
	}
	return Issue(ctx, housekeeping, key, now, validity)
}
