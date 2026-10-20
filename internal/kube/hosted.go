package kube

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/client-go/rest"
)

// HostedConnection is the Context of every hosted principal: the cluster the
// console runs in.
const HostedConnection = "in-cluster"

// HostedFactory acts for the accounts of one hosted configuration, each as
// its own Kubernetes username by impersonation, and for nobody else. It
// holds no client that acts as the console's own ServiceAccount, so no user
// call can fall back to one.
type HostedFactory struct {
	generation uint64
	clients    map[string]*Client
}

// NewHostedFactory builds one client per account from base, the console's
// own configuration. usernames maps each account ID to the Kubernetes
// username it acts as. Each client impersonates that username alone: no
// groups, no UID and no extra, so the API server's RBAC for the username is
// all the account may do. generation is the configuration's, and a principal
// from another one is refused.
func NewHostedFactory(base *rest.Config, usernames map[string]string, generation uint64) (*HostedFactory, error) {
	if base == nil {
		return nil, errors.New("kube: the console's own configuration is required")
	}
	impersonation := base.Impersonate
	if impersonation.UserName != "" || impersonation.UID != "" || len(impersonation.Groups) > 0 || len(impersonation.Extra) > 0 {
		return nil, errors.New("kube: the console's own configuration already impersonates someone")
	}
	if len(usernames) == 0 {
		return nil, errors.New("kube: a hosted console needs at least one account")
	}
	clients := make(map[string]*Client, len(usernames))
	for account, username := range usernames {
		switch {
		case account == "":
			return nil, errors.New("kube: an account needs an ID")
		case username == "":
			return nil, fmt.Errorf("kube: account %q has no username", account)
		case strings.HasPrefix(username, "system:"):
			return nil, fmt.Errorf("kube: account %q may not act as %q, a name Kubernetes keeps for itself", account, username)
		}
		config := rest.CopyConfig(base)
		config.Impersonate = rest.ImpersonationConfig{UserName: username}
		client, err := NewClient(config)
		if err != nil {
			return nil, fmt.Errorf("kube: account %q: %w", account, err)
		}
		clients[account] = client
	}
	return &HostedFactory{generation: generation, clients: clients}, nil
}

// For returns the client of the principal's account, and refuses a principal
// of another mode, connection or generation, or an account it does not have.
func (f *HostedFactory) For(principal Principal) (*Client, error) {
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	switch {
	case principal.Mode != ModeHosted:
		return nil, fmt.Errorf("kube: a hosted console cannot act for a %s principal", principal.Mode)
	case principal.Context != HostedConnection:
		return nil, fmt.Errorf("kube: a hosted console acts through %q, not %q", HostedConnection, principal.Context)
	case principal.Generation != f.generation:
		return nil, fmt.Errorf("kube: the configuration is generation %d, not %d", f.generation, principal.Generation)
	}
	client, ok := f.clients[principal.AccountID]
	if !ok {
		return nil, fmt.Errorf("kube: account %q is not in this console's configuration", principal.AccountID)
	}
	return client, nil
}
