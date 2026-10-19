package session_test

import (
	"testing"
	"testing/synctest"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/auth/session"
	"github.com/stokaro/ptah-console/internal/kube"
)

var local = kube.Principal{Mode: kube.ModeLocal, Context: "production", Generation: 1}

// step is one moment in a session's life: wait, optionally act, then look.
type step struct {
	wait    time.Duration
	use     session.Use
	expired bool
}

// The bubble's clock moves only by the waits below, so every instant a step
// looks at is distinct from the one the session was stamped with.
func TestASessionEndsOnItsOwnClock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []step
	}{
		{
			name:  "idle for 29 minutes is still alive",
			steps: []step{{wait: 29 * time.Minute}},
		},
		{
			name:  "idle for 30 minutes has ended",
			steps: []step{{wait: 30 * time.Minute, expired: true}},
		},
		{
			name: "a person's action restarts the idle timeout",
			steps: []step{
				{wait: 20 * time.Minute, use: session.Action},
				{wait: 29 * time.Minute},
			},
		},
		{
			name: "a background refresh does not",
			steps: []step{
				{wait: 20 * time.Minute},
				{wait: 10 * time.Minute, expired: true},
			},
		},
		{
			name:  "steady activity still ends at eight hours",
			steps: append(repeat(step{wait: 20 * time.Minute, use: session.Action}, 23), step{wait: 20 * time.Minute, use: session.Action, expired: true}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				c := qt.New(t)
				store := session.NewStore(session.Options{})
				token, created, err := store.Create(local)
				c.Assert(err, qt.IsNil)

				var last error
				for _, s := range test.steps {
					time.Sleep(s.wait)
					_, last = store.Lookup(token, s.use)
					c.Assert(last != nil, qt.Equals, s.expired, qt.Commentf("after %s: %v", s.wait, last))
				}

				c.Assert(created.Context().Err() != nil, qt.Equals, test.steps[len(test.steps)-1].expired)
			})
		})
	}
}

func repeat(s step, n int) []step {
	steps := make([]step, n)
	for i := range steps {
		steps[i] = s
	}
	return steps
}

// An expired session's streams close even when nobody looks it up again.
func TestSweepEndsWhatExpired(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := qt.New(t)
		store := session.NewStore(session.Options{})
		_, old, err := store.Create(local)
		c.Assert(err, qt.IsNil)
		time.Sleep(20 * time.Minute)
		_, fresh, err := store.Create(local)
		c.Assert(err, qt.IsNil)
		time.Sleep(10 * time.Minute)

		ended := store.Sweep()

		c.Assert(ended, qt.Equals, 1)
		c.Assert(old.Context().Err(), qt.IsNotNil)
		c.Assert(fresh.Context().Err(), qt.IsNil)
		c.Assert(store.Len(), qt.Equals, 1)
	})
}

func TestRevokeEndsTheSessionAndItsContext(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	store := session.NewStore(session.Options{})
	token, created, err := store.Create(local)
	c.Assert(err, qt.IsNil)

	store.Revoke(token)
	_, err = store.Lookup(token, session.Action)

	c.Assert(err, qt.ErrorIs, session.ErrNoSession)
	c.Assert(created.Context().Err(), qt.IsNotNil)
}

// Signing in replaces a session: the old token stops working, so one learned
// before sign-in is worth nothing after it.
func TestRotateReplacesTheToken(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	store := session.NewStore(session.Options{})
	old, before, err := store.Create(local)
	c.Assert(err, qt.IsNil)

	replacement, after, err := store.Rotate(old)
	c.Assert(err, qt.IsNil)
	_, lookupOld := store.Lookup(old, session.Background)

	c.Assert(replacement, qt.Not(qt.Equals), old)
	c.Assert(lookupOld, qt.ErrorIs, session.ErrNoSession)
	c.Assert(before.Context().Err(), qt.IsNotNil)
	c.Assert(after.Principal(), qt.Equals, local)
}

func TestRevokeWhereEndsOnlyTheMatchingSessions(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	store := session.NewStore(session.Options{})
	alice := kube.Principal{Mode: kube.ModeHosted, AccountID: "a-1", Context: "this-cluster"}
	bob := kube.Principal{Mode: kube.ModeHosted, AccountID: "a-2", Context: "this-cluster"}
	_, aliceSession, err := store.Create(alice)
	c.Assert(err, qt.IsNil)
	_, bobSession, err := store.Create(bob)
	c.Assert(err, qt.IsNil)

	ended := store.RevokeWhere(func(p kube.Principal) bool { return p.AccountID == "a-1" })

	c.Assert(ended, qt.Equals, 1)
	c.Assert(aliceSession.Context().Err(), qt.IsNotNil)
	c.Assert(bobSession.Context().Err(), qt.IsNil)
}

// A full store makes room by sweeping what expired, and otherwise refuses.
func TestCreateRefusesWhenTheStoreIsFull(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := qt.New(t)
		store := session.NewStore(session.Options{MaxSessions: 2})
		for range 2 {
			_, _, err := store.Create(local)
			c.Assert(err, qt.IsNil)
		}

		_, _, full := store.Create(local)
		time.Sleep(31 * time.Minute)
		_, _, afterExpiry := store.Create(local)

		c.Assert(full, qt.ErrorIs, session.ErrFull)
		c.Assert(afterExpiry, qt.IsNil)
		c.Assert(store.Len(), qt.Equals, 1)
	})
}

func TestCreateRefusesAPrincipalThatCannotAct(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	store := session.NewStore(session.Options{})

	_, created, err := store.Create(kube.Principal{Mode: kube.ModeHosted, Context: "this-cluster"})

	c.Assert(err, qt.IsNotNil)
	c.Assert(created, qt.IsNil)
	c.Assert(store.Len(), qt.Equals, 0)
}
