// Package kube builds the Kubernetes clients the console acts through.
//
// A client is bound to one identity when it is built and never changes: it
// owns its own copy of the REST configuration, and nothing rewrites headers
// on a shared one per request. A user request reaches Kubernetes only through
// the client of that user's Principal, and there is no client without one.
package kube

import (
	"errors"
	"fmt"

	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Mode is how the console reaches Kubernetes.
type Mode string

// Modes.
const (
	// ModeLocal acts with the person's own kubeconfig.
	ModeLocal Mode = "local"
	// ModeHosted impersonates the person's mapped identity.
	ModeHosted Mode = "hosted"
)

// Principal is whom a request acts for. It comes from the verified session
// and the server's configuration, never from the request.
type Principal struct {
	Mode Mode
	// AccountID is the Console account in hosted mode; empty in local mode.
	AccountID string
	// Context names the kubeconfig context in local mode, or the connection
	// in hosted mode.
	Context string
	// Generation changes when the context or its credentials change, which
	// invalidates anything decided under the previous one.
	Generation uint64
}

// Validate refuses a principal no client can be built for.
func (p Principal) Validate() error {
	switch p.Mode {
	case ModeLocal:
		if p.AccountID != "" {
			return errors.New("kube: a local principal has no Console account")
		}
	case ModeHosted:
		if p.AccountID == "" {
			return errors.New("kube: a hosted principal needs a Console account")
		}
	default:
		return fmt.Errorf("kube: unknown mode %q", p.Mode)
	}
	if p.Context == "" {
		return errors.New("kube: a principal needs a context")
	}
	return nil
}

// Client is everything that acts as one identity.
type Client struct {
	client.Client
	// Config is this identity's own copy. Callers read it to build a
	// subresource client, such as one for Pod logs, and never change it.
	Config *rest.Config
}

// ClientFactory hands out the client of a principal.
type ClientFactory interface {
	// For returns the client that acts as the principal, or an error when
	// the factory cannot act for it. It never returns a client for anyone
	// else instead.
	For(principal Principal) (*Client, error)
}

// Scheme knows the core kinds, the authentication and authorization reviews,
// and the operator's kinds.
func Scheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("kube: register core kinds: %w", err)
	}
	if err := operatorv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("kube: register operator kinds: %w", err)
	}
	return scheme, nil
}

// NewClient builds a client from its own copy of config. The caller's config
// is not kept, so a later change to it reaches no client already built.
func NewClient(config *rest.Config) (*Client, error) {
	if config == nil {
		return nil, errors.New("kube: a REST configuration is required")
	}
	own := rest.CopyConfig(config)
	scheme, err := Scheme()
	if err != nil {
		return nil, err
	}
	c, err := client.New(own, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("kube: build client: %w", err)
	}
	return &Client{Client: c, Config: own}, nil
}
