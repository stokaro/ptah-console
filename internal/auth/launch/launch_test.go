package launch_test

import (
	"testing"
	"testing/synctest"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/auth/launch"
)

func TestATokenIsGoodOnceAndOnlyInTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		redeem  func(l *launch.Launcher, token string) error
		refused bool
	}{
		{
			name:   "the token, in time",
			redeem: func(l *launch.Launcher, token string) error { return l.Redeem(token) },
		},
		{
			name: "the token a second time",
			redeem: func(l *launch.Launcher, token string) error {
				_ = l.Redeem(token)
				return l.Redeem(token)
			},
			refused: true,
		},
		{
			name: "the token after a minute",
			redeem: func(l *launch.Launcher, token string) error {
				time.Sleep(time.Minute)
				return l.Redeem(token)
			},
			refused: true,
		},
		{
			name:    "another token",
			redeem:  func(l *launch.Launcher, _ string) error { return l.Redeem("AAAA") },
			refused: true,
		},
		{
			name: "an earlier token after a new one was issued",
			redeem: func(l *launch.Launcher, token string) error {
				_, _ = l.Issue()
				return l.Redeem(token)
			},
			refused: true,
		},
		{
			name: "the empty token before any was issued",
			redeem: func(_ *launch.Launcher, _ string) error {
				return launch.New(0).Redeem("")
			},
			refused: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				c := qt.New(t)
				launcher := launch.New(time.Minute)
				token, err := launcher.Issue()
				c.Assert(err, qt.IsNil)

				err = test.redeem(launcher, token)

				c.Assert(err != nil, qt.Equals, test.refused, qt.Commentf("error: %v", err))
			})
		})
	}
}

// A wrong guess does not use up the real token, so somebody guessing cannot
// lock the person out; a late but correct one does, so it cannot be retried.
func TestAWrongGuessLeavesTheTokenGood(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	launcher := launch.New(time.Minute)
	token, err := launcher.Issue()
	c.Assert(err, qt.IsNil)

	c.Assert(launcher.Redeem("wrong"), qt.ErrorIs, launch.ErrRefused)
	c.Assert(launcher.Redeem(token), qt.IsNil)
}

func TestTokensCarry256Bits(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	launcher := launch.New(0)

	first, err := launcher.Issue()
	c.Assert(err, qt.IsNil)
	second, err := launcher.Issue()
	c.Assert(err, qt.IsNil)

	c.Assert(first, qt.HasLen, 43) // 32 bytes, unpadded base64url
	c.Assert(first, qt.Not(qt.Equals), second)
}
