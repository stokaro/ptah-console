// Package oidc signs people in to a hosted console with an OpenID Connect
// provider, and keeps each person's ID token valid while the session lasts.
//
// The console does not decide who anyone is. It checks the ID token as the
// specification asks, signature, issuer, audience, expiry and nonce, and
// hands the token to the API server, which maps its claims to a Kubernetes
// identity and lets RBAC decide. Sign-in is the authorization code flow with
// PKCE; each attempt is kept on the server and used once.
package oidc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

// CallbackPath is where the provider sends the browser back, under the
// console's public URL.
const CallbackPath = "/auth/oidc/callback"

// LoginPath starts a sign-in.
const LoginPath = "/auth/oidc/login"

// requestTimeout bounds one request to the provider.
const requestTimeout = 10 * time.Second

// Provider is the issuer as this console signs people in with it.
type Provider struct {
	name     string
	issuer   string
	oauth    oauth2.Config
	verifier *gooidc.IDTokenVerifier
	// client is what every request to the provider goes through: discovery,
	// keys, the token endpoint. It trusts the configured CA only, when one is
	// given.
	client *http.Client
}

// NewProvider reads the issuer's discovery document and returns the provider
// a sign-in goes through. clientSecret is empty for a public client, which
// PKCE protects. now is the clock ID tokens' expiry is read by; nil is
// time.Now.
func NewProvider(ctx context.Context, config *accounts.OIDC, publicURL, clientSecret string, now func() time.Time) (*Provider, error) {
	if config == nil {
		return nil, errors.New("oidc: no provider is configured")
	}
	client, err := httpClient(config.IssuerCA)
	if err != nil {
		return nil, err
	}
	discovered, err := gooidc.NewProvider(gooidc.ClientContext(ctx, client), config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: read the discovery document of %s: %w", config.IssuerURL, err)
	}
	var claims struct {
		ChallengeMethods []string `json:"code_challenge_methods_supported"`
	}
	if err := discovered.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: read the discovery document of %s: %w", config.IssuerURL, err)
	}
	// A provider that names its methods and leaves S256 out would reject or
	// ignore the challenge; one that names none is asked anyway.
	if len(claims.ChallengeMethods) > 0 && !slices.Contains(claims.ChallengeMethods, "S256") {
		return nil, fmt.Errorf("oidc: %s does not support PKCE with S256, which the console requires", config.IssuerURL)
	}
	endpoint := discovered.Endpoint()
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" {
		return nil, fmt.Errorf("oidc: the discovery document of %s names no authorization or token endpoint", config.IssuerURL)
	}
	name := config.DisplayName
	if name == "" {
		name = config.IssuerURL
	}
	return &Provider{
		name:   name,
		issuer: config.IssuerURL,
		oauth: oauth2.Config{
			ClientID: config.ClientID, ClientSecret: clientSecret, Endpoint: endpoint,
			RedirectURL: publicURL + CallbackPath,
			Scopes:      append([]string{gooidc.ScopeOpenID}, config.ExtraScopes...),
		},
		verifier: discovered.VerifierContext(gooidc.ClientContext(context.WithoutCancel(ctx), client),
			&gooidc.Config{ClientID: config.ClientID, Now: now}),
		client: client,
	}, nil
}

// Name is how the sign-in page names the provider.
func (p *Provider) Name() string { return p.name }

// Issuer is the issuer the provider's ID tokens name.
func (p *Provider) Issuer() string { return p.issuer }

// ClientID is the audience the API server has to trust.
func (p *Provider) ClientID() string { return p.oauth.ClientID }

// context carries the provider's HTTP client to the oauth2 package.
func (p *Provider) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, p.client)
}

// httpClient trusts ca alone when it is given, and the system's roots
// otherwise.
func httpClient(ca string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("oidc: the default transport is not an *http.Transport")
	}
	transport = transport.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if ca != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(ca)) {
			return nil, errors.New("oidc: issuerCA holds no PEM certificate")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &http.Client{Transport: transport, Timeout: requestTimeout}, nil
}

// Identity is what a verified ID token says, for the session and the page.
// Nothing in it decides access: the API server reads the token itself.
type Identity struct {
	// Subject is the provider's own ID for the person; a renewed token has to
	// name the same one.
	Subject string
	// Name is how the page names the person, from the token's name or email
	// claim when it has one.
	Name string
}

// verify checks a raw ID token and, when nonce is given, that it carries it.
func (p *Provider) verify(ctx context.Context, raw, nonce string) (*gooidc.IDToken, Identity, error) {
	token, err := p.verifier.Verify(p.context(ctx), raw)
	if err != nil {
		return nil, Identity{}, fmt.Errorf("oidc: the ID token does not verify: %w", err)
	}
	if nonce != "" && token.Nonce != nonce {
		return nil, Identity{}, errors.New("oidc: the ID token does not carry this sign-in's nonce")
	}
	var claims struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, Identity{}, fmt.Errorf("oidc: read the ID token's claims: %w", err)
	}
	name := claims.Name
	if name == "" {
		name = claims.Email
	}
	return token, Identity{Subject: token.Subject, Name: name}, nil
}
