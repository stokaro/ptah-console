package ratelimit_test

import (
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/auth/ratelimit"
)

var start = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func attempts(limiter *ratelimit.Limiter, key string, at time.Time, n int) []bool {
	allowed := make([]bool, n)
	for i := range n {
		allowed[i] = limiter.Allow(key, at)
	}
	return allowed
}

func TestAKeyGetsItsBurstAndThenOneAttemptPerInterval(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	limiter := ratelimit.New(10*time.Second, 3, 100)

	c.Assert(attempts(limiter, "alex", start, 4), qt.DeepEquals, []bool{true, true, true, false})
	c.Assert(attempts(limiter, "alex", start.Add(9*time.Second), 1), qt.DeepEquals, []bool{false})
	c.Assert(attempts(limiter, "alex", start.Add(10*time.Second), 2), qt.DeepEquals, []bool{true, false})
	c.Assert(attempts(limiter, "sam", start, 1), qt.DeepEquals, []bool{true})
}

// A full table forgets a key whose bucket has refilled, which loses nothing.
func TestAFullTableForgetsAKeyThatHasRefilled(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	limiter := ratelimit.New(10*time.Second, 2, 2)
	c.Assert(attempts(limiter, "alex", start, 2), qt.DeepEquals, []bool{true, true})
	c.Assert(attempts(limiter, "sam", start.Add(15*time.Second), 2), qt.DeepEquals, []bool{true, true})

	// alex has refilled by now and sam has not.
	c.Assert(attempts(limiter, "kim", start.Add(20*time.Second), 1), qt.DeepEquals, []bool{true})
	c.Assert(limiter.Len(), qt.Equals, 2)
	c.Assert(attempts(limiter, "sam", start.Add(20*time.Second), 1), qt.DeepEquals, []bool{false})
}

// A full table of keys that are all still limited refuses a new key, so
// spreading attempts over many keys never resets the one being guessed.
func TestAFullTableOfLimitedKeysRefusesANewKey(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	limiter := ratelimit.New(10*time.Second, 2, 2)
	c.Assert(attempts(limiter, "alex", start, 3), qt.DeepEquals, []bool{true, true, false})
	c.Assert(attempts(limiter, "sam", start, 1), qt.DeepEquals, []bool{true})

	c.Assert(attempts(limiter, "kim", start.Add(time.Second), 1), qt.DeepEquals, []bool{false})
	c.Assert(attempts(limiter, "alex", start.Add(time.Second), 1), qt.DeepEquals, []bool{false})
	c.Assert(limiter.Len(), qt.Equals, 2)
}
