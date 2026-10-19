// Package launch issues the single-use token ptah-console local opens the
// browser with.
//
// The token travels in the URL fragment, which a browser never sends to a
// server, so it reaches no access log and no Referer header. The page trades
// it for a session cookie and removes it from history. A token is good once,
// for one minute; the launcher keeps only its digest.
package launch

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// DefaultTTL is how long a token is good for.
const DefaultTTL = time.Minute

// ErrRefused is the one answer to a token that is not good: wrong, used or
// expired look the same to the caller.
var ErrRefused = errors.New("launch: the token is not valid")

// Launcher holds the outstanding token. Issuing a new one replaces it.
type Launcher struct {
	ttl time.Duration

	mu      sync.Mutex
	digest  [sha256.Size]byte
	expires time.Time
	issued  bool
}

// New returns a launcher with no outstanding token.
func New(ttl time.Duration) *Launcher {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Launcher{ttl: ttl}
}

// Issue returns a new 256-bit token and forgets any earlier one.
func (l *Launcher) Issue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.digest = sha256.Sum256([]byte(token))
	l.expires = time.Now().Add(l.ttl)
	l.issued = true
	return token, nil
}

// Redeem accepts the outstanding token once, before it expires.
func (l *Launcher) Redeem(token string) error {
	presented := sha256.Sum256([]byte(token))
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.issued {
		return ErrRefused
	}
	match := subtle.ConstantTimeCompare(presented[:], l.digest[:]) == 1
	if !match {
		return ErrRefused
	}
	// A matching token is used up whether or not it was still in time.
	l.issued = false
	if !time.Now().Before(l.expires) {
		return ErrRefused
	}
	return nil
}
