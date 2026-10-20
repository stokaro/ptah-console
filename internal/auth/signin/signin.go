// Package signin checks a hosted account's login and password.
//
// Every refusal looks the same from outside. An unknown login is compared
// against a hash made at start, at the cost the accounts' hashes use, so it
// takes as long as a wrong password, and a password longer than bcrypt reads
// is compared too before it is refused. Attempts are limited per login and
// per source address before any hash is compared.
package signin

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/auth/ratelimit"
)

// Errors Verify returns.
var (
	ErrRefused   = errors.New("signin: the login or the password is wrong")
	ErrThrottled = errors.New("signin: too many attempts")
)

// Limits is one rate limit: Burst attempts at once, one more every Every,
// for at most MaxKeys logins or addresses.
type Limits struct {
	Every   time.Duration
	Burst   int
	MaxKeys int
}

// Default limits. A person who mistypes gets five tries a minute; a source
// address, which can be a proxy many people share, gets thirty.
var (
	DefaultPerLogin   = Limits{Every: 12 * time.Second, Burst: 5, MaxKeys: 10000}
	DefaultPerAddress = Limits{Every: 2 * time.Second, Burst: 30, MaxKeys: 10000}
)

// maxLoginKeyBytes is the longest login kept as a key of its own. A longer
// one cannot be an account's, and all of them share one bucket.
const maxLoginKeyBytes = 254

// Options configure a Verifier.
type Options struct {
	Config    *accounts.Config
	Passwords *accounts.Passwords
	// PerLogin and PerAddress default to DefaultPerLogin and
	// DefaultPerAddress when zero.
	PerLogin, PerAddress Limits
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Compare checks a password against a hash; nil is
	// bcrypt.CompareHashAndPassword.
	Compare func(hash, password []byte) error
}

// Verifier checks sign-in attempts.
type Verifier struct {
	byLogin   map[string]accounts.Account
	passwords *accounts.Passwords
	dummy     []byte
	logins    *ratelimit.Limiter
	addresses *ratelimit.Limiter
	now       func() time.Time
	compare   func(hash, password []byte) error
}

// New builds a Verifier for one configuration and its passwords.
func New(options Options) (*Verifier, error) {
	if options.Config == nil || options.Passwords == nil || len(options.Config.Accounts) == 0 {
		return nil, errors.New("signin: a configuration with accounts and its passwords are required")
	}
	byLogin := make(map[string]accounts.Account, len(options.Config.Accounts))
	cost := accounts.MinCost
	for _, account := range options.Config.Accounts {
		hash, ok := options.Passwords.Hash(account.ID)
		if !ok {
			return nil, fmt.Errorf("signin: account %q has no password", account.ID)
		}
		if accountCost, err := bcrypt.Cost(hash); err == nil {
			cost = max(cost, accountCost)
		}
		byLogin[strings.ToLower(account.Login)] = account
	}
	dummy, err := dummyHash(cost)
	if err != nil {
		return nil, err
	}
	perLogin, perAddress := options.PerLogin, options.PerAddress
	if perLogin == (Limits{}) {
		perLogin = DefaultPerLogin
	}
	if perAddress == (Limits{}) {
		perAddress = DefaultPerAddress
	}
	verifier := &Verifier{
		byLogin:   byLogin,
		passwords: options.Passwords,
		dummy:     dummy,
		logins:    ratelimit.New(perLogin.Every, perLogin.Burst, perLogin.MaxKeys),
		addresses: ratelimit.New(perAddress.Every, perAddress.Burst, perAddress.MaxKeys),
		now:       options.Now,
		compare:   options.Compare,
	}
	if verifier.now == nil {
		verifier.now = time.Now
	}
	if verifier.compare == nil {
		verifier.compare = bcrypt.CompareHashAndPassword
	}
	return verifier, nil
}

// dummyHash is the hash of a random password nobody knows, at the highest
// cost the accounts use, for an unknown login to be compared against.
func dummyHash(cost int) ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("signin: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword(secret, cost)
	if err != nil {
		return nil, fmt.Errorf("signin: %w", err)
	}
	return hash, nil
}

// Verify returns the account a login and password belong to. address is
// where the attempt came from. It returns ErrThrottled before comparing
// anything when the login or the address has tried too often, and
// ErrRefused for every other failure.
func (v *Verifier) Verify(login, password, address string) (accounts.Account, error) {
	key := strings.ToLower(login)
	if len(key) > maxLoginKeyBytes {
		key = "\x00 too long"
	}
	now := v.now()
	if !v.addresses.Allow(address, now) || !v.logins.Allow(key, now) {
		return accounts.Account{}, ErrThrottled
	}
	account, known := v.byLogin[key]
	hash := v.dummy
	if known {
		hash, _ = v.passwords.Hash(account.ID)
	}
	// Compared whether or not the login is known and whatever the password's
	// length, so neither shows in how long the answer takes. bcrypt reads
	// the first 72 bytes; one more is enough for CheckPassword to refuse.
	attempt := []byte(password)
	matched := v.compare(hash, attempt[:min(len(attempt), accounts.MaxPasswordBytes+1)]) == nil
	if !known || !matched || accounts.CheckPassword(attempt) != nil {
		return accounts.Account{}, ErrRefused
	}
	return account, nil
}
