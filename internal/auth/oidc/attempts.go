package oidc

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Bounds on the sign-ins in flight.
const (
	// AttemptLifetime is how long a person has at the provider before the
	// attempt is forgotten.
	AttemptLifetime = 10 * time.Minute
	// MaxAttempts bounds the attempts kept at once, so a flood of sign-in
	// starts cannot grow the table without bound.
	MaxAttempts = 1024
)

// Errors a caller tells apart.
var (
	// ErrUnknownAttempt is a state this console did not start, or one it
	// already finished or forgot.
	ErrUnknownAttempt = errors.New("oidc: no sign-in is waiting under this state")
	// ErrTooManyAttempts is a full table.
	ErrTooManyAttempts = errors.New("oidc: too many sign-ins are in progress")
)

// attempt is one sign-in waiting for the provider: what the callback has to
// see again.
type attempt struct {
	nonce    string
	verifier string
	started  time.Time
}

// attempts holds the sign-ins in flight, each under its state, each used
// once.
type attempts struct {
	now func() time.Time

	mu      sync.Mutex
	byState map[string]attempt
}

func newAttempts(now func() time.Time) *attempts {
	return &attempts{now: now, byState: make(map[string]attempt)}
}

// begin keeps a new attempt and returns its state, nonce and PKCE verifier.
func (a *attempts) begin() (state string, kept attempt, err error) {
	state, err = random()
	if err != nil {
		return "", attempt{}, err
	}
	nonce, err := random()
	if err != nil {
		return "", attempt{}, err
	}
	kept = attempt{nonce: nonce, verifier: oauth2.GenerateVerifier(), started: a.now()}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked()
	if len(a.byState) >= MaxAttempts {
		return "", attempt{}, ErrTooManyAttempts
	}
	a.byState[state] = kept
	return state, kept, nil
}

// take returns the attempt under state and forgets it, so a callback that
// comes twice finishes once.
func (a *attempts) take(state string) (attempt, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept, ok := a.byState[state]
	delete(a.byState, state)
	if !ok || a.now().Sub(kept.started) > AttemptLifetime {
		return attempt{}, ErrUnknownAttempt
	}
	return kept, nil
}

func (a *attempts) sweepLocked() {
	now := a.now()
	for state, kept := range a.byState {
		if now.Sub(kept.started) > AttemptLifetime {
			delete(a.byState, state)
		}
	}
}

// random is 256 bits from the system's generator, URL-safe.
func random() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
