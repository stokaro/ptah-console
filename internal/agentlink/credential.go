// Package agentlink is the console's side of the agent channel: the
// credentials it issues and accepts, the channels agents open, and the
// requests the console sends down them.
//
// The console keeps a verifier of each credential, never the credential: the
// SHA-256 of 256 random bits, in the connection's registry entry, which only
// the console's own identity may write. A channel is bound to its
// connection's UID and generation, and raising the generation ends every
// channel opened under the old one.
package agentlink

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
)

// Credential lifetimes.
const (
	// DefaultValidity is how long a credential is accepted.
	DefaultValidity = 90 * 24 * time.Hour
	// ExpiryWarning is how long before expiry the console says so.
	ExpiryWarning = 14 * 24 * time.Hour
	// RotationOverlap is the longest an older credential is accepted once a
	// newer one has authenticated a channel.
	RotationOverlap = time.Hour
)

// ErrRotationInProgress refuses a third credential: a rotation has to finish,
// or its unused credential expire, first.
var ErrRotationInProgress = errors.New("agentlink: the connection already has two credentials; finish the rotation first")

// Verifier is what the console keeps of a credential.
func Verifier(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

// newCredential is 256 random bits, as the agent's Secret holds it.
func newCredential() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// accepted is the credential among the entry's that the presented one
// matches and that is still accepted at now, compared in constant time.
func accepted(entry *consolev1alpha1.ConsoleCluster, presented string, now time.Time) (*consolev1alpha1.ChannelCredential, bool) {
	if entry.Status.Channel == nil {
		return nil, false
	}
	verifier := []byte(Verifier(presented))
	var match *consolev1alpha1.ChannelCredential
	for index := range entry.Status.Channel.Credentials {
		credential := &entry.Status.Channel.Credentials[index]
		if subtle.ConstantTimeCompare(verifier, []byte(credential.Verifier)) == 1 {
			match = credential
		}
	}
	if match == nil || !live(match, now) {
		return nil, false
	}
	return match, true
}

// live reports whether a credential is accepted at now.
func live(credential *consolev1alpha1.ChannelCredential, now time.Time) bool {
	return now.Before(credential.NotAfter.Time) && (credential.RetireAt == nil || now.Before(credential.RetireAt.Time))
}

// endOf is when a credential stops being accepted.
func endOf(credential *consolev1alpha1.ChannelCredential) time.Time {
	if credential.RetireAt != nil && credential.RetireAt.Before(&credential.NotAfter) {
		return credential.RetireAt.Time
	}
	return credential.NotAfter.Time
}

// Issue gives an Agent connection a new credential, valid for validity from
// now, and returns it. The console keeps only its verifier; the credential is
// shown once, to be written into the agent's Secret. A connection that has
// none yet starts past any generation a quarantine fences, and a quarantined
// one moves past the fence with this credential alone, which lifts the
// quarantine once the agent runs with it. A removed connection gets none.
func Issue(ctx context.Context, housekeeping client.Client, key client.ObjectKey, now time.Time, validity time.Duration) (string, error) {
	credential, err := newCredential()
	if err != nil {
		return "", err
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry := &consolev1alpha1.ConsoleCluster{}
		if err := housekeeping.Get(ctx, key, entry); err != nil {
			return err
		}
		if entry.Spec.Profile != consolev1alpha1.ProfileAgent {
			return fmt.Errorf("agentlink: %s is a %s connection; only an agent has a channel", key, entry.Spec.Profile)
		}
		fence, err := FenceOf(ctx, housekeeping, key.Namespace, entry.Spec.ConnectionUID)
		if err != nil {
			return err
		}
		switch {
		case fence.Removed:
			return ErrRemoved
		case entry.Status.Channel == nil:
			entry.Status.Channel = &consolev1alpha1.ChannelStatus{Generation: fence.Through + 1}
		case fence.Fences(entry.Status.Channel.Generation):
			// Lifting a quarantine: new credentials at a generation past the
			// fence, and none of the old ones.
			entry.Status.Channel.Generation = fence.Through + 1
			entry.Status.Channel.Credentials = nil
		}
		if len(entry.Status.Channel.Credentials) >= 2 {
			return ErrRotationInProgress
		}
		entry.Status.Channel.Credentials = append(entry.Status.Channel.Credentials, consolev1alpha1.ChannelCredential{
			Verifier: Verifier(credential),
			IssuedAt: metav1.NewTime(now), NotAfter: metav1.NewTime(now.Add(validity)),
		})
		return housekeeping.Status().Update(ctx, entry)
	})
	if err != nil {
		return "", err
	}
	return credential, nil
}

// Tend drops the credentials an Agent connection no longer accepts and says
// whether the one in use expires within ExpiryWarning. It writes only when
// something changed.
func Tend(ctx context.Context, housekeeping client.Client, key client.ObjectKey, now time.Time) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry := &consolev1alpha1.ConsoleCluster{}
		if err := housekeeping.Get(ctx, key, entry); err != nil {
			return err
		}
		if entry.Status.Channel == nil {
			return nil
		}
		before := entry.DeepCopy()
		kept := entry.Status.Channel.Credentials[:0]
		for _, credential := range entry.Status.Channel.Credentials {
			if live(&credential, now) {
				kept = append(kept, credential)
			}
		}
		entry.Status.Channel.Credentials = kept
		meta.SetStatusCondition(&entry.Status.Conditions, expiring(entry, now))
		if equalStatus(before, entry) {
			return nil
		}
		return housekeeping.Status().Update(ctx, entry)
	})
}

// inUse is the credential that last authenticated a channel, or the only
// one when none has.
func inUse(entry *consolev1alpha1.ConsoleCluster) *consolev1alpha1.ChannelCredential {
	var current *consolev1alpha1.ChannelCredential
	for index := range entry.Status.Channel.Credentials {
		credential := &entry.Status.Channel.Credentials[index]
		switch {
		case credential.FirstUsedAt == nil:
		case current == nil || current.FirstUsedAt == nil || current.FirstUsedAt.Before(credential.FirstUsedAt):
			current = credential
		}
	}
	if current == nil && len(entry.Status.Channel.Credentials) == 1 {
		current = &entry.Status.Channel.Credentials[0]
	}
	return current
}

// expiring is the CredentialExpiring condition at now.
func expiring(entry *consolev1alpha1.ConsoleCluster, now time.Time) metav1.Condition {
	condition := metav1.Condition{Type: consolev1alpha1.ConditionRotationDue, ObservedGeneration: entry.Generation}
	current := inUse(entry)
	switch {
	case current == nil:
		condition.Status, condition.Reason = metav1.ConditionUnknown, "NoCredentialInUse"
		condition.Message = "no channel credential has authenticated a channel"
	case !now.Before(current.NotAfter.Add(-ExpiryWarning)):
		condition.Status, condition.Reason = metav1.ConditionTrue, "ExpiresSoon"
		condition.Message = "the channel credential in use expires at " + current.NotAfter.UTC().Format(time.RFC3339) + "; rotate it"
	default:
		condition.Status, condition.Reason = metav1.ConditionFalse, "Valid"
		condition.Message = "the channel credential in use expires at " + current.NotAfter.UTC().Format(time.RFC3339)
	}
	return condition
}

// equalStatus compares what Tend writes, ignoring the condition's transition
// time, which SetStatusCondition keeps when the status is unchanged.
func equalStatus(a, b *consolev1alpha1.ConsoleCluster) bool {
	if len(a.Status.Channel.Credentials) != len(b.Status.Channel.Credentials) {
		return false
	}
	old := meta.FindStatusCondition(a.Status.Conditions, consolev1alpha1.ConditionRotationDue)
	current := meta.FindStatusCondition(b.Status.Conditions, consolev1alpha1.ConditionRotationDue)
	return old != nil && current != nil && old.Status == current.Status && old.Reason == current.Reason &&
		old.Message == current.Message && old.ObservedGeneration == current.ObservedGeneration
}

// Raise ends every channel of the connection: it raises the generation, so an
// agent bound to the old one is refused until it is configured with the new.
func Raise(ctx context.Context, housekeeping client.Client, key client.ObjectKey) (int64, error) {
	var generation int64
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry := &consolev1alpha1.ConsoleCluster{}
		if err := housekeeping.Get(ctx, key, entry); err != nil {
			return err
		}
		if entry.Status.Channel == nil {
			return fmt.Errorf("agentlink: %s has no channel to fence", key)
		}
		entry.Status.Channel.Generation++
		generation = entry.Status.Channel.Generation
		return housekeeping.Status().Update(ctx, entry)
	})
	return generation, err
}

// TendAll tends every Agent entry of the registry in namespace every
// interval, until ctx ends. A failure is logged and tried again next time.
func TendAll(ctx context.Context, housekeeping client.Client, namespace string, interval time.Duration, now func() time.Time, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		entries := &consolev1alpha1.ConsoleClusterList{}
		err := housekeeping.List(ctx, entries, client.InNamespace(namespace))
		for index := range entries.Items {
			entry := &entries.Items[index]
			if entry.Spec.Profile != consolev1alpha1.ProfileAgent || entry.Status.Channel == nil {
				continue
			}
			if tendErr := Tend(ctx, housekeeping, client.ObjectKeyFromObject(entry), now()); tendErr != nil {
				err = tendErr
			}
		}
		if err != nil && ctx.Err() == nil {
			logger.Warn("the console could not tend its agents' credentials", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
