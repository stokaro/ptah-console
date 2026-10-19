package kube

import (
	"fmt"

	"k8s.io/client-go/rest"
)

// LocalFactory acts for exactly one principal: the person running
// ptah-console local, with the kubeconfig context resolved at start.
type LocalFactory struct {
	principal Principal
	client    *Client
}

// NewLocalFactory binds the factory to the principal and the configuration
// its context resolved to. The configuration is copied.
func NewLocalFactory(principal Principal, config *rest.Config) (*LocalFactory, error) {
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	if principal.Mode != ModeLocal {
		return nil, fmt.Errorf("kube: a local factory cannot act for a %s principal", principal.Mode)
	}
	c, err := NewClient(config)
	if err != nil {
		return nil, err
	}
	return &LocalFactory{principal: principal, client: c}, nil
}

// For returns the client when the principal is the one the factory was built
// for, and refuses any other, including the same context under a newer
// generation.
func (f *LocalFactory) For(principal Principal) (*Client, error) {
	if principal != f.principal {
		return nil, fmt.Errorf("kube: this console acts for context %q generation %d, not %q generation %d",
			f.principal.Context, f.principal.Generation, principal.Context, principal.Generation)
	}
	return f.client, nil
}
