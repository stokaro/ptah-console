package kube

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"k8s.io/client-go/rest"
)

// Bearer is what authenticates one person's requests: it sets the person's
// current token on each.
type Bearer interface {
	Transport(next http.RoundTripper) http.RoundTripper
}

// OIDCFactory acts for the people signed in with an OpenID Connect provider,
// each with their own ID token and nothing else. It holds no client that
// acts as the console's ServiceAccount, so no user call can fall back to one.
type OIDCFactory struct {
	base       *rest.Config
	generation uint64

	mu      sync.Mutex
	clients map[string]*Client
}

// NewOIDCFactory keeps the API server's address and how to verify it from
// cluster, the console's own configuration, and nothing that authenticates:
// not its token or token file, a client certificate, an exec or auth
// plugin, or an impersonation. A configuration that does not verify the
// API server is refused, since a person's token would go to whoever
// answers.
func NewOIDCFactory(cluster *rest.Config, generation uint64) (*OIDCFactory, error) {
	if cluster == nil {
		return nil, errors.New("kube: the console's own configuration is required")
	}
	if cluster.Insecure {
		return nil, errors.New("kube: the API server's certificate is not verified; a person's token is sent only to a server the console verified")
	}
	base := &rest.Config{
		Host:    cluster.Host,
		APIPath: cluster.APIPath,
		TLSClientConfig: rest.TLSClientConfig{
			ServerName: cluster.ServerName,
			CAFile:     cluster.CAFile,
			CAData:     cluster.CAData,
			NextProtos: cluster.NextProtos,
		},
		UserAgent: cluster.UserAgent,
		QPS:       cluster.QPS,
		Burst:     cluster.Burst,
		Timeout:   cluster.Timeout,
	}
	return &OIDCFactory{base: base, generation: generation, clients: make(map[string]*Client)}, nil
}

// Add builds the client of one sign-in, which authenticates every request
// with bearer alone.
func (f *OIDCFactory) Add(signIn string, bearer Bearer) error {
	if signIn == "" || bearer == nil {
		return errors.New("kube: a sign-in needs an ID and a bearer")
	}
	config := rest.CopyConfig(f.base)
	config.Wrap(bearer.Transport)
	client, err := NewClient(config)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, taken := f.clients[signIn]; taken {
		return fmt.Errorf("kube: sign-in %q already has a client", signIn)
	}
	f.clients[signIn] = client
	return nil
}

// Remove forgets the client of a sign-in that ended.
func (f *OIDCFactory) Remove(signIn string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.clients, signIn)
}

// For returns the client of the principal's sign-in, and refuses a principal
// of another mode, connection or generation, or a sign-in that ended.
func (f *OIDCFactory) For(principal Principal) (*Client, error) {
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	switch {
	case principal.Mode != ModeOIDC:
		return nil, fmt.Errorf("kube: an OIDC console cannot act for a %s principal", principal.Mode)
	case principal.Context != HostedConnection:
		return nil, fmt.Errorf("kube: an OIDC console acts through %q, not %q", HostedConnection, principal.Context)
	case principal.Generation != f.generation:
		return nil, fmt.Errorf("kube: the configuration is generation %d, not %d", f.generation, principal.Generation)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	client, ok := f.clients[principal.AccountID]
	if !ok {
		return nil, fmt.Errorf("kube: sign-in %q has ended", principal.AccountID)
	}
	return client, nil
}
