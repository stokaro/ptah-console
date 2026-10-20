package signin_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	"golang.org/x/crypto/bcrypt"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/auth/signin"
)

const configuration = `apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: prod
publicURL: https://console.example.com
namespaces: [storefront]
accounts:
  - id: alex
    login: alex@example.com
  - id: sam
    login: sam@example.com
`

var (
	alexPassword = "correct horse battery staple"
	// samPassword is as long as bcrypt reads, so one byte more is a password
	// bcrypt would cut back to this one.
	samPassword = strings.Repeat("s", accounts.MaxPasswordBytes)
)

var directory = sync.OnceValues(func() (*accounts.Config, error) {
	return accounts.ParseConfig([]byte(configuration))
})

var passwordsFile = sync.OnceValues(func() (string, error) {
	lines := ""
	for _, account := range []struct{ id, password string }{{"alex", alexPassword}, {"sam", samPassword}} {
		hash, err := accounts.Hash([]byte(account.password), accounts.MinCost)
		if err != nil {
			return "", err
		}
		lines += account.id + ":" + string(hash) + "\n"
	}
	return lines, nil
})

// clock is a fake time a test moves by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// comparisons records every hash a Verifier compared against.
type comparisons struct {
	mu     sync.Mutex
	hashes []string
}

func (c *comparisons) Compare(hash, password []byte) error {
	c.mu.Lock()
	c.hashes = append(c.hashes, string(hash))
	c.mu.Unlock()
	return bcrypt.CompareHashAndPassword(hash, password)
}

func (c *comparisons) Hashes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.hashes...)
}

type fixture struct {
	verifier    *signin.Verifier
	clock       *clock
	comparisons *comparisons
	passwords   *accounts.Passwords
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	c := qt.New(t)
	config, err := directory()
	c.Assert(err, qt.IsNil)
	file, err := passwordsFile()
	c.Assert(err, qt.IsNil)
	passwords, err := accounts.ParsePasswords([]byte(file), config)
	c.Assert(err, qt.IsNil)
	f := fixture{
		clock:       &clock{now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)},
		comparisons: &comparisons{},
		passwords:   passwords,
	}
	f.verifier, err = signin.New(signin.Options{
		Config: config, Passwords: passwords, Now: f.clock.Now, Compare: f.comparisons.Compare,
	})
	c.Assert(err, qt.IsNil)
	return f
}

func TestTheRightPasswordSignsInTheAccountOfTheLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, login, password, account string
	}{
		{name: "alex", login: "alex@example.com", password: alexPassword, account: "alex"},
		{name: "the login in another case", login: "Alex@Example.COM", password: alexPassword, account: "alex"},
		{name: "a password as long as bcrypt reads", login: "sam@example.com", password: samPassword, account: "sam"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			f := newFixture(t)

			account, err := f.verifier.Verify(test.login, test.password, "192.0.2.1")

			c.Assert(err, qt.IsNil)
			c.Assert(account.ID, qt.Equals, test.account)
		})
	}
}

// Every refusal is the same error, and every one compares a hash first, so
// the answer does not say whether the login exists.
func TestEveryRefusalIsTheSameAndComparesAHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, login, password string
	}{
		{name: "a wrong password", login: "alex@example.com", password: "correct horse"},
		{name: "another account's password", login: "alex@example.com", password: samPassword},
		{name: "an unknown login", login: "kim@example.com", password: alexPassword},
		{name: "an empty password", login: "alex@example.com", password: ""},
		{name: "one byte past what bcrypt reads", login: "sam@example.com", password: samPassword + "s"},
		{name: "a login longer than any account's", login: strings.Repeat("k", 300), password: alexPassword},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			f := newFixture(t)

			account, err := f.verifier.Verify(test.login, test.password, "192.0.2.1")

			c.Assert(err, qt.ErrorIs, signin.ErrRefused)
			c.Assert(err.Error(), qt.Equals, "signin: the login or the password is wrong")
			c.Assert(account, qt.DeepEquals, accounts.Account{})
			c.Assert(f.comparisons.Hashes(), qt.HasLen, 1)
		})
	}
}

// An unknown login is compared against a hash that is no account's, made at
// the cost the accounts' hashes use.
func TestAnUnknownLoginIsComparedAgainstAHashOfTheSameCost(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	f := newFixture(t)

	_, err := f.verifier.Verify("kim@example.com", alexPassword, "192.0.2.1")

	c.Assert(err, qt.ErrorIs, signin.ErrRefused)
	compared := f.comparisons.Hashes()
	c.Assert(compared, qt.HasLen, 1)
	alex, _ := f.passwords.Hash("alex")
	sam, _ := f.passwords.Hash("sam")
	c.Assert(compared[0], qt.Not(qt.Equals), string(alex))
	c.Assert(compared[0], qt.Not(qt.Equals), string(sam))
	cost, err := bcrypt.Cost([]byte(compared[0]))
	c.Assert(err, qt.IsNil)
	c.Assert(cost, qt.Equals, accounts.MinCost)
}

// After five attempts a login waits, even with the right password, and
// without a hash being compared; another login is not held back.
func TestALoginThatTriedTooOftenWaits(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	f := newFixture(t)
	for range signin.DefaultPerLogin.Burst {
		_, err := f.verifier.Verify("alex@example.com", "guess", "192.0.2.1")
		c.Assert(err, qt.ErrorIs, signin.ErrRefused)
	}

	_, throttled := f.verifier.Verify("ALEX@example.com", alexPassword, "192.0.2.2")
	_, other := f.verifier.Verify("sam@example.com", samPassword, "192.0.2.1")
	f.clock.Advance(signin.DefaultPerLogin.Every)
	_, later := f.verifier.Verify("alex@example.com", alexPassword, "192.0.2.1")

	c.Assert(throttled, qt.ErrorIs, signin.ErrThrottled)
	c.Assert(other, qt.IsNil)
	c.Assert(later, qt.IsNil)
	c.Assert(f.comparisons.Hashes(), qt.HasLen, signin.DefaultPerLogin.Burst+2)
}

// One address that tries many logins waits too.
func TestAnAddressThatTriedTooOftenWaits(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	f := newFixture(t)
	for i := range signin.DefaultPerAddress.Burst {
		_, err := f.verifier.Verify("user"+strings.Repeat("x", i)+"@example.com", "guess", "198.51.100.7")
		c.Assert(err, qt.ErrorIs, signin.ErrRefused)
	}

	_, throttled := f.verifier.Verify("alex@example.com", alexPassword, "198.51.100.7")
	_, elsewhere := f.verifier.Verify("alex@example.com", alexPassword, "192.0.2.1")

	c.Assert(throttled, qt.ErrorIs, signin.ErrThrottled)
	c.Assert(elsewhere, qt.IsNil)
}

func TestAVerifierNeedsAccountsAndTheirPasswords(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	verifier, err := signin.New(signin.Options{})

	c.Assert(verifier, qt.IsNil)
	c.Assert(err, qt.ErrorMatches, "signin: a configuration with accounts and its passwords are required")
}
