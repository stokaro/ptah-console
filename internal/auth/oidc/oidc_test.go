package oidc_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/auth/oidc"
	"github.com/stokaro/ptah-console/internal/auth/oidc/oidctest"
)

const publicURL = "https://console.example.com"

var alex = oidctest.Person{Subject: "subject-alex", Email: "alex@example.com", Name: "Alex Morgan"}

// clock is a fake clock the console and the provider share.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// lab is a provider and a console's signer for it, on one fake clock.
type lab struct {
	issuer *oidctest.Provider
	signer *oidc.Signer
	clock  *clock
}

func newLab(t *testing.T, change func(*oidctest.Provider)) lab {
	t.Helper()
	c := qt.New(t)
	issuer, err := oidctest.New("ptah-console")
	c.Assert(err, qt.IsNil)
	t.Cleanup(issuer.Server.Close)
	at := &clock{now: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}
	issuer.Now = at.Now
	if change != nil {
		change(issuer)
	}
	provider, err := oidc.NewProvider(t.Context(), &accounts.OIDC{
		IssuerURL: issuer.URL(), ClientID: "ptah-console", IssuerCA: issuer.CA(),
	}, publicURL, "", at.Now)
	c.Assert(err, qt.IsNil)
	return lab{issuer: issuer, signer: oidc.NewSigner(provider, at.Now), clock: at}
}

// callback is where the provider sends the browser back: its state and code.
func callback(t *testing.T, back string) (state, code string) {
	t.Helper()
	address, err := url.Parse(back)
	qt.New(t).Assert(err, qt.IsNil)
	qt.New(t).Assert(address.Scheme+"://"+address.Host+address.Path, qt.Equals, publicURL+oidc.CallbackPath)
	return address.Query().Get("state"), address.Query().Get("code")
}

// signIn runs one whole sign-in as person.
func (l lab) signIn(t *testing.T, person oidctest.Person) (*oidc.Credential, oidc.Identity) {
	t.Helper()
	c := qt.New(t)
	authorization, _, err := l.signer.Begin()
	c.Assert(err, qt.IsNil)
	back, err := l.issuer.Approve(authorization, person)
	c.Assert(err, qt.IsNil)
	state, code := callback(t, back)
	credential, identity, err := l.signer.Finish(t.Context(), state, code)
	c.Assert(err, qt.IsNil)
	return credential, identity
}

// A sign-in asks for the code with an S256 challenge, a state and a nonce,
// and ends with an ID token for the person who signed in.
func TestASignInEndsWithThePersonsIDToken(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, nil)

	authorization, state, err := l.signer.Begin()
	c.Assert(err, qt.IsNil)
	query := mustQuery(t, authorization)
	c.Assert(query.Get("state"), qt.Equals, state)
	c.Assert(query.Get("code_challenge_method"), qt.Equals, "S256")
	c.Assert(query.Get("nonce"), qt.Not(qt.Equals), "")
	c.Assert(query.Get("scope"), qt.Equals, "openid")
	c.Assert(query.Get("redirect_uri"), qt.Equals, publicURL+oidc.CallbackPath)
	back, err := l.issuer.Approve(authorization, alex)
	c.Assert(err, qt.IsNil)
	returned, code := callback(t, back)

	credential, identity, err := l.signer.Finish(t.Context(), returned, code)

	c.Assert(err, qt.IsNil)
	c.Assert(identity, qt.DeepEquals, oidc.Identity{Subject: "subject-alex", Name: "Alex Morgan"})
	token, err := credential.Token(t.Context())
	c.Assert(err, qt.IsNil)
	c.Assert(strings.Count(token, "."), qt.Equals, 2)
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	address, err := url.Parse(raw)
	qt.New(t).Assert(err, qt.IsNil)
	return address.Query()
}

// A state finishes its sign-in once: the callback that comes again reaches
// neither the provider nor a session.
func TestAStateFinishesOneSignInOnce(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, nil)
	authorization, _, err := l.signer.Begin()
	c.Assert(err, qt.IsNil)
	back, err := l.issuer.Approve(authorization, alex)
	c.Assert(err, qt.IsNil)
	state, code := callback(t, back)
	_, _, err = l.signer.Finish(t.Context(), state, code)
	c.Assert(err, qt.IsNil)

	_, _, err = l.signer.Finish(t.Context(), state, code)

	c.Assert(err, qt.ErrorIs, oidc.ErrUnknownAttempt)
	c.Assert(l.issuer.Exchanges, qt.Equals, 1)
}

// A state is forgotten after ten minutes, and one the console never started
// finishes nothing; neither reaches the provider.
func TestAStateTheConsoleDoesNotHoldIsRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		wait  time.Duration
		state func(state string) string
	}{
		{name: "after ten minutes", wait: oidc.AttemptLifetime + time.Second, state: func(state string) string { return state }},
		{name: "a state the console never started", state: func(string) string { return "forged" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			l := newLab(t, nil)
			authorization, _, err := l.signer.Begin()
			c.Assert(err, qt.IsNil)
			back, err := l.issuer.Approve(authorization, alex)
			c.Assert(err, qt.IsNil)
			state, code := callback(t, back)
			l.clock.Add(test.wait)

			_, _, err = l.signer.Finish(t.Context(), test.state(state), code)

			c.Assert(err, qt.ErrorIs, oidc.ErrUnknownAttempt)
			c.Assert(l.issuer.Exchanges, qt.Equals, 0)
		})
	}
}

// What the provider sends back is checked before anyone is signed in: the
// PKCE verifier, the token's audience and issuer, and the nonce.
func TestASignInRefusesWhatDoesNotCheckOut(t *testing.T) {
	t.Parallel()
	keepProvider := func(*oidctest.Provider) {}
	keepURL := func(url.Values) {}
	tests := []struct {
		name     string
		provider func(*oidctest.Provider)
		url      func(url.Values)
		says     string
	}{
		{name: "another challenge", provider: keepProvider, url: func(q url.Values) { q.Set("code_challenge", "forged") },
			says: `oidc: trade the code for tokens: .*invalid_grant.*`},
		{name: "another nonce", provider: keepProvider, url: func(q url.Values) { q.Set("nonce", "forged") },
			says: `oidc: the ID token does not carry this sign-in's nonce`},
		{name: "another audience", provider: func(p *oidctest.Provider) { p.Audience = "another-client" }, url: keepURL,
			says: `oidc: the ID token does not verify: .*audience.*`},
		{name: "another issuer", provider: func(p *oidctest.Provider) { p.Issuer = "https://elsewhere.example.com" }, url: keepURL,
			says: `oidc: the ID token does not verify: .*issued by a different provider.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			l := newLab(t, nil)
			test.provider(l.issuer)
			authorization, _, err := l.signer.Begin()
			c.Assert(err, qt.IsNil)
			address, err := url.Parse(authorization)
			c.Assert(err, qt.IsNil)
			query := address.Query()
			test.url(query)
			address.RawQuery = query.Encode()
			back, err := l.issuer.Approve(address.String(), alex)
			c.Assert(err, qt.IsNil)
			state, code := callback(t, back)

			credential, _, err := l.signer.Finish(t.Context(), state, code)

			c.Assert(credential, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

// A provider that names its PKCE methods without S256, and one whose
// certificate the console does not trust, are refused at start.
func TestAProviderIsRefusedAtStart(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	issuer, err := oidctest.New("ptah-console")
	c.Assert(err, qt.IsNil)
	t.Cleanup(issuer.Server.Close)

	_, err = oidc.NewProvider(t.Context(), &accounts.OIDC{IssuerURL: issuer.URL(), ClientID: "ptah-console"}, publicURL, "", nil)
	c.Assert(err, qt.ErrorMatches, `oidc: read the discovery document of .*: .*certificate.*`)

	issuer.ChallengeMethods = []string{"plain"}
	_, err = oidc.NewProvider(t.Context(), &accounts.OIDC{
		IssuerURL: issuer.URL(), ClientID: "ptah-console", IssuerCA: issuer.CA(),
	}, publicURL, "", nil)
	c.Assert(err, qt.ErrorMatches, `oidc: .* does not support PKCE with S256, which the console requires`)
}

// Near its expiry the token is renewed with the refresh token, checked as
// the first one was, and the new one is used from then on.
func TestATokenIsRenewedBeforeItExpires(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, nil)
	credential, _ := l.signIn(t, alex)
	first, err := credential.Token(t.Context())
	c.Assert(err, qt.IsNil)

	l.clock.Add(time.Hour - oidc.RenewBefore - time.Second)
	still, err := credential.Token(t.Context())
	c.Assert(err, qt.IsNil)
	c.Assert(still, qt.Equals, first)
	c.Assert(l.issuer.Refreshes, qt.Equals, 0)

	l.clock.Add(2 * time.Second)
	renewed, err := credential.Token(t.Context())

	c.Assert(err, qt.IsNil)
	c.Assert(l.issuer.Refreshes, qt.Equals, 1)
	c.Assert(renewed, qt.Not(qt.Equals), first)
	l.clock.Add(30 * time.Minute)
	again, err := credential.Token(t.Context())
	c.Assert(err, qt.IsNil)
	c.Assert(again, qt.Equals, renewed)
}

// ends counts how often, and why, a credential ended.
type ends struct {
	mu      sync.Mutex
	reasons []error
}

func (e *ends) record(reason error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reasons = append(e.reasons, reason)
}

func (e *ends) all() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.reasons...)
}

// A credential that can no longer give a token the API server accepts
// ends, once, with why, and gives no token after.
func TestACredentialEndsWhenItCannotBeKeptValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider func(*oidctest.Provider)
		reason   error
		says     string
	}{
		{name: "no refresh token", provider: func(p *oidctest.Provider) { p.NoRefresh = true }, reason: oidc.ErrExpired,
			says: `oidc: the sign-in's token expired and the provider gave no way to renew it`},
		{name: "a refresh the provider refuses", provider: func(p *oidctest.Provider) { p.RefreshFails = true },
			reason: oidc.ErrRenewal, says: `.*invalid_grant.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			l := newLab(t, test.provider)
			credential, _ := l.signIn(t, alex)
			ended := &ends{}
			credential.OnEnd(ended.record)

			l.clock.Add(time.Hour - oidc.RenewBefore + time.Second)
			_, err := credential.Token(t.Context())
			c.Assert(err, qt.IsNil, qt.Commentf("the token still holds, so it is still given"))
			c.Assert(ended.all(), qt.HasLen, 0)

			l.clock.Add(oidc.RenewBefore)
			_, err = credential.Token(t.Context())

			c.Assert(err, qt.ErrorIs, test.reason)
			c.Assert(err, qt.ErrorMatches, test.says)
			_, again := credential.Token(t.Context())
			c.Assert(again, qt.ErrorIs, test.reason)
			c.Assert(ended.all(), qt.HasLen, 1)
			c.Assert(ended.all()[0], qt.ErrorIs, test.reason)
		})
	}
}

// A renewal for another person ends the credential at once, although the
// token in hand still holds.
func TestARenewalForAnotherPersonEndsTheSignInAtOnce(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, func(p *oidctest.Provider) { p.RefreshAs = "subject-sam" })
	credential, _ := l.signIn(t, alex)
	ended := &ends{}
	credential.OnEnd(ended.record)
	l.clock.Add(time.Hour - oidc.RenewBefore + time.Second)

	_, err := credential.Token(t.Context())

	c.Assert(err, qt.ErrorIs, oidc.ErrAnotherSubject)
	c.Assert(ended.all(), qt.HasLen, 1)
	_, err = credential.Token(t.Context())
	c.Assert(err, qt.ErrorIs, oidc.ErrAnotherSubject)
	c.Assert(l.issuer.RefreshAttempts, qt.Equals, 1)
}

// A renewal that fails while the token still holds is tried again ten
// seconds later, not on every request, and the next one that works renews.
func TestAFailedRenewalWaitsBeforeTheNext(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, func(p *oidctest.Provider) { p.RefreshFails = true })
	credential, _ := l.signIn(t, alex)
	l.clock.Add(time.Hour - oidc.RenewBefore + time.Second)

	for range 5 {
		_, err := credential.Token(t.Context())
		c.Assert(err, qt.IsNil)
	}
	c.Assert(l.issuer.RefreshAttempts, qt.Equals, 1)
	l.clock.Add(10 * time.Second)
	_, err := credential.Token(t.Context())
	c.Assert(err, qt.IsNil)
	c.Assert(l.issuer.RefreshAttempts, qt.Equals, 2)
	l.issuer.RefreshFails = false
	l.clock.Add(10 * time.Second)

	_, err = credential.Token(t.Context())

	c.Assert(err, qt.IsNil)
	c.Assert(l.issuer.RefreshAttempts, qt.Equals, 3)
	c.Assert(l.issuer.Refreshes, qt.Equals, 1)
}

// Every request carries the person's ID token and nothing else that
// authenticates, and an answer of 401 ends the credential.
func TestTheTransportSendsTheTokenAndEndsOnARefusal(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, nil)
	credential, _ := l.signIn(t, alex)
	token, err := credential.Token(t.Context())
	c.Assert(err, qt.IsNil)
	var mu sync.Mutex
	var seen []string
	status := http.StatusOK
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Header.Get("Authorization"))
		w.WriteHeader(status)
	}))
	t.Cleanup(api.Close)
	client := &http.Client{Transport: credential.Transport(http.DefaultTransport)}
	ended := &ends{}
	credential.OnEnd(ended.record)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, api.URL, nil)
	c.Assert(err, qt.IsNil)
	request.Header.Set("Authorization", "Bearer someone-else")
	response, err := client.Do(request)
	c.Assert(err, qt.IsNil)
	_ = response.Body.Close()
	c.Assert(request.Header.Get("Authorization"), qt.Equals, "Bearer someone-else", qt.Commentf("the caller's request is not changed"))
	c.Assert(ended.all(), qt.HasLen, 0)

	mu.Lock()
	status = http.StatusUnauthorized
	mu.Unlock()
	response, err = client.Get(api.URL)
	c.Assert(err, qt.IsNil)
	_ = response.Body.Close()
	_, err = client.Get(api.URL)

	c.Assert(err, qt.ErrorIs, oidc.ErrRefused)
	c.Assert(seen, qt.DeepEquals, []string{"Bearer " + token, "Bearer " + token})
	c.Assert(ended.all(), qt.HasLen, 1)
	c.Assert(ended.all()[0], qt.ErrorIs, oidc.ErrRefused)
}

// The name the page shows has no control characters and is bounded.
func TestTheNameIsSafeToShow(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	l := newLab(t, nil)

	_, identity := l.signIn(t, oidctest.Person{Subject: "s", Name: "Alex\x1b[2J " + strings.Repeat("é", 100)})

	c.Assert(strings.ContainsRune(identity.Name, '\x1b'), qt.IsFalse)
	c.Assert(len(identity.Name) <= 128, qt.IsTrue)
	c.Assert(strings.HasPrefix(identity.Name, "Alex[2J é"), qt.IsTrue)
}
