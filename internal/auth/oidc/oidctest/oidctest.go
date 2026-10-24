// Package oidctest is an OpenID Connect provider for tests: discovery, keys,
// and a token endpoint that holds the authorization code flow to PKCE and
// signs real ID tokens. A test plays the person at the provider by calling
// Approve with the authorization URL the console sent the browser to.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Person is who signs in at the provider.
type Person struct {
	Subject string
	Email   string
	Name    string
}

// Provider is a running test issuer.
type Provider struct {
	Server *httptest.Server
	// ClientID is the only client the provider serves.
	ClientID string
	// Lifetime is how long an ID token holds; zero is an hour.
	Lifetime time.Duration

	key    *rsa.PrivateKey
	signer jose.Signer

	mu sync.Mutex
	// Now is the provider's clock; nil is time.Now.
	Now func() time.Time
	// Audience overrides the audience tokens carry, to test a refusal.
	Audience string
	// Issuer overrides the issuer tokens name, to test a refusal.
	Issuer string
	// ChallengeMethods are the PKCE methods discovery names; nil is S256.
	ChallengeMethods []string
	// NoRefresh issues no refresh token.
	NoRefresh bool
	// RefreshAs makes a refresh name another subject, to test a refusal.
	RefreshAs string
	// RefreshFails makes every refresh fail.
	RefreshFails bool
	// Exchanges counts the codes traded, Refreshes the refresh tokens traded,
	// and RefreshAttempts every refresh asked for, refused or not.
	Exchanges, Refreshes, RefreshAttempts int
	codes                                 map[string]grant
	refresh                               map[string]Person
}

type grant struct {
	person    Person
	nonce     string
	challenge string
	redirect  string
}

// New starts a provider for clientID over TLS. Close it with Server.Close.
func New(clientID string) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		return nil, err
	}
	p := &Provider{ClientID: clientID, key: key, signer: signer,
		codes: make(map[string]grant), refresh: make(map[string]Person)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("POST /token", p.token)
	p.Server = httptest.NewTLSServer(mux)
	return p, nil
}

// URL is the issuer.
func (p *Provider) URL() string { return p.Server.URL }

// CA is the provider's certificate in PEM, for a client to trust.
func (p *Provider) CA() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.Server.Certificate().Raw}))
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	methods := p.ChallengeMethods
	p.mu.Unlock()
	if methods == nil {
		methods = []string{"S256"}
	}
	writeJSON(w, map[string]any{
		"issuer":                                p.URL(),
		"authorization_endpoint":                p.URL() + "/authorize",
		"token_endpoint":                        p.URL() + "/token",
		"jwks_uri":                              p.URL() + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      methods,
	})
}

func (p *Provider) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: "test", Algorithm: string(jose.RS256), Use: "sig",
	}}})
}

// Approve is the person signing in at the provider: it reads the
// authorization URL the console sent the browser to and returns the
// callback URL the provider would send it back to, with a code.
func (p *Provider) Approve(authorizationURL string, person Person) (string, error) {
	address, err := url.Parse(authorizationURL)
	if err != nil {
		return "", err
	}
	query := address.Query()
	switch {
	case query.Get("client_id") != p.ClientID:
		return "", fmt.Errorf("oidctest: client %q is not %q", query.Get("client_id"), p.ClientID)
	case query.Get("response_type") != "code":
		return "", fmt.Errorf("oidctest: response type %q is not code", query.Get("response_type"))
	case query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "":
		return "", fmt.Errorf("oidctest: the request carries no S256 challenge")
	}
	code := randomString()
	p.mu.Lock()
	p.codes[code] = grant{person: person, nonce: query.Get("nonce"),
		challenge: query.Get("code_challenge"), redirect: query.Get("redirect_uri")}
	p.mu.Unlock()
	back, err := url.Parse(query.Get("redirect_uri"))
	if err != nil {
		return "", err
	}
	values := back.Query()
	values.Set("code", code)
	values.Set("state", query.Get("state"))
	back.RawQuery = values.Encode()
	return back.String(), nil
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	client, _, _ := r.BasicAuth()
	if client == "" {
		client = r.PostForm.Get("client_id")
	}
	if client != p.ClientID {
		oauthError(w, "invalid_client")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		g, ok := p.codes[r.PostForm.Get("code")]
		delete(p.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge || r.PostForm.Get("redirect_uri") != g.redirect {
			oauthError(w, "invalid_grant")
			return
		}
		p.Exchanges++
		p.issue(w, g.person, g.nonce)
	case "refresh_token":
		p.RefreshAttempts++
		person, ok := p.refresh[r.PostForm.Get("refresh_token")]
		if !ok || p.RefreshFails {
			oauthError(w, "invalid_grant")
			return
		}
		// A refresh token is used once; the answer carries the next.
		delete(p.refresh, r.PostForm.Get("refresh_token"))
		p.Refreshes++
		if p.RefreshAs != "" {
			person.Subject = p.RefreshAs
		}
		p.issue(w, person, "")
	default:
		oauthError(w, "unsupported_grant_type")
	}
}

// issue answers with a new ID token for person, and a refresh token unless
// the provider gives none. It runs with mu held.
func (p *Provider) issue(w http.ResponseWriter, person Person, nonce string) {
	raw, err := p.IDToken(person, nonce)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	answer := map[string]any{
		"access_token": randomString(), "token_type": "Bearer", "expires_in": 3600, "id_token": raw,
	}
	if !p.NoRefresh {
		refresh := randomString()
		p.refresh[refresh] = person
		answer["refresh_token"] = refresh
	}
	writeJSON(w, answer)
}

// IDToken signs an ID token for person, as the provider would issue it.
func (p *Provider) IDToken(person Person, nonce string) (string, error) {
	lifetime := p.Lifetime
	if lifetime == 0 {
		lifetime = time.Hour
	}
	issuer, audience := p.URL(), p.ClientID
	if p.Issuer != "" {
		issuer = p.Issuer
	}
	if p.Audience != "" {
		audience = p.Audience
	}
	now := p.now()
	claims := map[string]any{
		"iss": issuer, "aud": audience, "sub": person.Subject,
		"iat": now.Unix(), "exp": now.Add(lifetime).Unix(),
		"email": person.Email, "email_verified": true, "name": person.Name,
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	return jwt.Signed(p.signer).Claims(claims).Serialize()
}

func oauthError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func randomString() string {
	buffer := make([]byte, 24)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}
