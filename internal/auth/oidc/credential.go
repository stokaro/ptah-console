package oidc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/oauth2"
)

const (
	// RenewBefore is how long before the ID token expires the console asks
	// the provider for a new one.
	RenewBefore = time.Minute
	// retryAfter spaces the attempts to renew while the token still holds.
	retryAfter = 10 * time.Second
	// maxNameBytes bounds the name the page shows.
	maxNameBytes = 128
)

// Why a credential ends. Each ends the session it belongs to.
var (
	ErrExpired = errors.New("oidc: the sign-in's token expired and the provider gave no way to renew it")
	ErrRenewal = errors.New("oidc: the provider did not renew the sign-in's token")
	ErrRefused = errors.New("oidc: the API server no longer accepts the sign-in's token")
	// ErrAnotherSubject is a renewal for someone else. It ends the
	// credential at once, while the token in hand still holds: the
	// provider answered for a person this sign-in is not.
	ErrAnotherSubject = errors.New("oidc: the renewed ID token names another subject")
)

// Signer runs sign-ins against one provider.
type Signer struct {
	provider *Provider
	attempts *attempts
	now      func() time.Time
}

// NewSigner returns the signer of provider. A nil now is time.Now.
func NewSigner(provider *Provider, now func() time.Time) *Signer {
	if now == nil {
		now = time.Now
	}
	return &Signer{provider: provider, attempts: newAttempts(now), now: now}
}

// Provider is the provider the signer signs in with.
func (s *Signer) Provider() *Provider { return s.provider }

// Begin starts a sign-in. It returns where to send the browser and the state
// the browser's own cookie has to carry back.
func (s *Signer) Begin() (authURL, state string, err error) {
	state, kept, err := s.attempts.begin()
	if err != nil {
		return "", "", err
	}
	return s.provider.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(kept.verifier),
		oauth2.SetAuthURLParam("nonce", kept.nonce)), state, nil
}

// Finish ends the attempt under state: it trades the code for tokens, with
// the attempt's PKCE verifier, and checks the ID token and its nonce.
func (s *Signer) Finish(ctx context.Context, state, code string) (*Credential, Identity, error) {
	kept, err := s.attempts.take(state)
	if err != nil {
		return nil, Identity{}, err
	}
	if code == "" {
		return nil, Identity{}, errors.New("oidc: the provider sent no code")
	}
	token, err := s.provider.oauth.Exchange(s.provider.context(ctx), code, oauth2.VerifierOption(kept.verifier))
	if err != nil {
		return nil, Identity{}, fmt.Errorf("oidc: trade the code for tokens: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, Identity{}, errors.New("oidc: the provider sent no ID token")
	}
	idToken, identity, err := s.provider.verify(ctx, raw, kept.nonce)
	if err != nil {
		return nil, Identity{}, err
	}
	identity.Name = sanitized(identity.Name)
	return &Credential{
		provider: s.provider, subject: identity.Subject, now: s.now,
		raw: raw, expiry: idToken.Expiry, refresh: token.RefreshToken,
	}, identity, nil
}

// Credential is one person's sign-in: the ID token the API server reads,
// and the refresh token that renews it. It ends when it can no longer give
// a token the API server accepts, and never gives another person's.
type Credential struct {
	provider *Provider
	subject  string
	now      func() time.Time

	mu       sync.Mutex
	raw      string
	expiry   time.Time
	refresh  string
	failedAt time.Time
	ended    error
	onEnd    []func(error)
}

// OnEnd runs f once the credential ends, with why; at once if it already has.
func (c *Credential) OnEnd(f func(error)) {
	c.mu.Lock()
	ended := c.ended
	if ended == nil {
		c.onEnd = append(c.onEnd, f)
	}
	c.mu.Unlock()
	if ended != nil {
		f(ended)
	}
}

// End ends the credential. Later calls keep the first reason.
func (c *Credential) End(reason error) {
	c.mu.Lock()
	hooks := c.endLocked(reason)
	c.mu.Unlock()
	for _, hook := range hooks {
		hook(reason)
	}
}

func (c *Credential) endLocked(reason error) []func(error) {
	if c.ended != nil {
		return nil
	}
	c.ended, c.raw, c.refresh = reason, "", ""
	hooks := c.onEnd
	c.onEnd = nil
	return hooks
}

// Token is an ID token the API server can read now. Near its expiry it is
// renewed first; a renewal that fails is tried again while the token still
// holds, and ends the credential once it does not.
func (c *Credential) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	raw, hooks, err := c.tokenLocked(ctx)
	c.mu.Unlock()
	for _, hook := range hooks {
		hook(err)
	}
	return raw, err
}

func (c *Credential) tokenLocked(ctx context.Context) (string, []func(error), error) {
	if c.ended != nil {
		return "", nil, c.ended
	}
	now := c.now()
	valid := now.Before(c.expiry)
	if now.Before(c.expiry.Add(-RenewBefore)) {
		return c.raw, nil, nil
	}
	if c.refresh == "" {
		if valid {
			return c.raw, nil, nil
		}
		return "", c.endLocked(ErrExpired), ErrExpired
	}
	if valid && !c.failedAt.IsZero() && now.Sub(c.failedAt) < retryAfter {
		return c.raw, nil, nil
	}
	if err := c.renewLocked(ctx); err != nil {
		if errors.Is(err, ErrAnotherSubject) {
			return "", c.endLocked(err), err
		}
		if valid {
			c.failedAt = now
			return c.raw, nil, nil
		}
		reason := fmt.Errorf("%w: %w", ErrRenewal, err)
		return "", c.endLocked(reason), reason
	}
	return c.raw, nil, nil
}

// renewLocked trades the refresh token for a new ID token and checks it as
// the first one was checked, for the same subject. A refresh response
// carries no nonce.
func (c *Credential) renewLocked(ctx context.Context) error {
	source := c.provider.oauth.TokenSource(c.provider.context(ctx), &oauth2.Token{RefreshToken: c.refresh})
	token, err := source.Token()
	if err != nil {
		return err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return errors.New("the provider sent no new ID token")
	}
	idToken, identity, err := c.provider.verify(ctx, raw, "")
	if err != nil {
		return err
	}
	if identity.Subject != c.subject {
		return ErrAnotherSubject
	}
	c.raw, c.expiry, c.failedAt = raw, idToken.Expiry, time.Time{}
	if token.RefreshToken != "" {
		c.refresh = token.RefreshToken
	}
	return nil
}

// Transport sends every request with the credential's ID token and nothing
// else that authenticates. A request the API server answers 401 ends the
// credential: the server stopped trusting the token, and no other identity
// stands in for it.
func (c *Credential) Transport(next http.RoundTripper) http.RoundTripper {
	return &bearer{next: next, credential: c}
}

type bearer struct {
	next       http.RoundTripper
	credential *Credential
}

func (b *bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	raw, err := b.credential.Token(request.Context())
	if err != nil {
		return nil, err
	}
	signed := request.Clone(request.Context())
	signed.Header.Set("Authorization", "Bearer "+raw)
	response, err := b.next.RoundTrip(signed)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		b.credential.End(ErrRefused)
	}
	return response, err
}

// sanitized is a name the page can show: without control characters, cut
// on a rune boundary to maxNameBytes.
func sanitized(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	for len(name) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}
