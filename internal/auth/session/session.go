// Package session keeps the console's sessions: server-side, in memory,
// behind an opaque token in a cookie.
//
// A session binds a browser to one kube.Principal. It ends after 30 minutes
// without a person's action or 8 hours after it began, whichever comes
// first, and on logout or revocation. Its context is cancelled when it ends,
// so every stream bound to it closes with it. Background refreshes do not
// count as activity; only requests the caller marks as a person's action do.
//
// The store keeps a digest of each token, not the token.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/stokaro/ptah-console/internal/kube"
)

// Defaults for the limits a zero Options leaves unset.
const (
	DefaultIdleTimeout    = 30 * time.Minute
	DefaultAbsoluteLimit  = 8 * time.Hour
	DefaultMaxSessions    = 1024
	DefaultMaxConcurrency = 8
)

// Errors a caller tells apart.
var (
	ErrNoSession = errors.New("session: no such session")
	ErrExpired   = errors.New("session: expired")
	ErrFull      = errors.New("session: too many sessions")
)

// Options configure a Store.
type Options struct {
	IdleTimeout   time.Duration
	AbsoluteLimit time.Duration
	MaxSessions   int
	// MaxConcurrency bounds the requests one session has in flight.
	MaxConcurrency int
}

// Use says whether a lookup is a person's action, which extends the idle
// timeout, or a background read, which does not.
type Use int

// Uses.
const (
	Background Use = iota
	Action
)

// Session is one signed-in browser.
type Session struct {
	principal kube.Principal
	started   time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	slots     chan struct{}

	mu         sync.Mutex
	lastAction time.Time
}

// Principal is whom the session's requests act for.
func (s *Session) Principal() kube.Principal { return s.principal }

// Context ends when the session does. A stream bound to it closes then.
func (s *Session) Context() context.Context { return s.ctx }

// Acquire takes one of the session's request slots, or reports that the
// session has as many requests in flight as it may.
func (s *Session) Acquire() (release func(), ok bool) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, true
	default:
		return nil, false
	}
}

// Store holds the sessions of one console process.
type Store struct {
	options Options

	mu       sync.Mutex
	sessions map[[sha256.Size]byte]*Session
}

// NewStore returns an empty store.
func NewStore(options Options) *Store {
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = DefaultIdleTimeout
	}
	if options.AbsoluteLimit <= 0 {
		options.AbsoluteLimit = DefaultAbsoluteLimit
	}
	if options.MaxSessions <= 0 {
		options.MaxSessions = DefaultMaxSessions
	}
	if options.MaxConcurrency <= 0 {
		options.MaxConcurrency = DefaultMaxConcurrency
	}
	return &Store{options: options, sessions: make(map[[sha256.Size]byte]*Session)}
}

// Create starts a session for the principal and returns its token. The
// token is what the cookie carries; the store keeps only its digest.
func (s *Store) Create(principal kube.Principal) (string, *Session, error) {
	if err := principal.Validate(); err != nil {
		return "", nil, err
	}
	token, err := newToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		principal:  principal,
		started:    now,
		lastAction: now,
		ctx:        ctx,
		cancel:     cancel,
		slots:      make(chan struct{}, s.options.MaxConcurrency),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= s.options.MaxSessions {
		s.sweepLocked(now)
	}
	if len(s.sessions) >= s.options.MaxSessions {
		cancel()
		return "", nil, ErrFull
	}
	s.sessions[digest(token)] = session
	return token, session, nil
}

// Lookup returns the token's session. A person's action extends the idle
// timeout; a background read leaves it.
func (s *Store) Lookup(token string, use Use) (*Session, error) {
	key := digest(token)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return nil, ErrNoSession
	}
	if s.expired(session, now) {
		s.endLocked(key, session)
		return nil, ErrExpired
	}
	if use == Action {
		session.mu.Lock()
		session.lastAction = now
		session.mu.Unlock()
	}
	return session, nil
}

// Rotate ends the token's session and starts a new one for the same
// principal, which is what signing in does to a session that already
// existed: a token somebody else learned before stops working.
func (s *Store) Rotate(token string) (string, *Session, error) {
	session, err := s.Lookup(token, Action)
	if err != nil {
		return "", nil, err
	}
	s.Revoke(token)
	return s.Create(session.principal)
}

// Revoke ends the token's session. Revoking an unknown token does nothing.
func (s *Store) Revoke(token string) {
	key := digest(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[key]; ok {
		s.endLocked(key, session)
	}
}

// RevokeWhere ends every session whose principal matches, such as every
// session of an account whose password changed. It returns how many ended.
func (s *Store) RevokeWhere(match func(kube.Principal) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	ended := 0
	for key, session := range s.sessions {
		if match(session.principal) {
			s.endLocked(key, session)
			ended++
		}
	}
	return ended
}

// Sweep ends every expired session, so their streams close even when nobody
// looks them up again. It returns how many ended.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweepLocked(time.Now())
}

// Run sweeps every interval until ctx ends.
func (s *Store) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Sweep()
		}
	}
}

// Len is the number of sessions held, expired ones not yet swept included.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func (s *Store) sweepLocked(now time.Time) int {
	ended := 0
	for key, session := range s.sessions {
		if s.expired(session, now) {
			s.endLocked(key, session)
			ended++
		}
	}
	return ended
}

func (s *Store) expired(session *Session, now time.Time) bool {
	session.mu.Lock()
	lastAction := session.lastAction
	session.mu.Unlock()
	return !now.Before(session.started.Add(s.options.AbsoluteLimit)) ||
		!now.Before(lastAction.Add(s.options.IdleTimeout))
}

func (s *Store) endLocked(key [sha256.Size]byte, session *Session) {
	delete(s.sessions, key)
	session.cancel()
}

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func digest(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}
