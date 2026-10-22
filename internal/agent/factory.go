package agent

import (
	"errors"
	"fmt"
	"sync/atomic"

	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/kube"
)

// Factory hands out the client of an allowlisted account: one that
// impersonates exactly the account's username, with no groups, UID or
// extras. It serves one connection, and the allowlist it reads is the one
// set last.
type Factory struct {
	base       *rest.Config
	config     *Configuration
	allowlist  atomic.Pointer[map[string]*kube.Client]
	generation uint64
}

// NewFactory returns a factory that acts through base, the agent's own
// identity, which may impersonate the allowlisted usernames and nothing else.
func NewFactory(base *rest.Config, config *Configuration) (*Factory, error) {
	if base == nil {
		return nil, errors.New("agent: the agent's own configuration is required")
	}
	impersonation := base.Impersonate
	if impersonation.UserName != "" || impersonation.UID != "" || len(impersonation.Groups) > 0 || len(impersonation.Extra) > 0 {
		return nil, errors.New("agent: the agent's own configuration already impersonates someone")
	}
	return &Factory{base: base, config: config, generation: config.Connection.Generation}, nil
}

// Set replaces the allowlist. A request already running keeps the client it
// has; the next one reads this list.
func (f *Factory) Set(list *Accounts) error {
	clients := make(map[string]*kube.Client, len(list.Accounts))
	for _, account := range list.Accounts {
		config := rest.CopyConfig(f.base)
		config.Impersonate = rest.ImpersonationConfig{UserName: f.config.Username(account)}
		client, err := kube.NewClient(config)
		if err != nil {
			return fmt.Errorf("agent: account %q: %w", account, err)
		}
		clients[account] = client
	}
	f.allowlist.Store(&clients)
	return nil
}

// Allowed reports whether the allowlist names the account.
func (f *Factory) Allowed(account string) bool {
	current := f.allowlist.Load()
	if current == nil {
		return false
	}
	_, ok := (*current)[account]
	return ok
}

// Principal is whom the agent acts for when a request names account: the
// account, on this connection and its generation.
func (f *Factory) Principal(account string) kube.Principal {
	return kube.Principal{Mode: kube.ModeHosted, AccountID: account, Context: f.config.Connection.UID, Generation: f.generation}
}

// For returns the client of the principal's account, and refuses a principal
// of another connection or generation, or an account the allowlist does not
// name.
func (f *Factory) For(principal kube.Principal) (*kube.Client, error) {
	if err := principal.Validate(); err != nil {
		return nil, err
	}
	switch {
	case principal.Mode != kube.ModeHosted:
		return nil, fmt.Errorf("agent: cannot act for a %s principal", principal.Mode)
	case principal.Context != f.config.Connection.UID:
		return nil, fmt.Errorf("agent: serves connection %s, not %s", f.config.Connection.UID, principal.Context)
	case principal.Generation != f.generation:
		return nil, fmt.Errorf("agent: serves generation %d, not %d", f.generation, principal.Generation)
	}
	current := f.allowlist.Load()
	if current == nil {
		return nil, errors.New("agent: no allowlist yet")
	}
	client, ok := (*current)[principal.AccountID]
	if !ok {
		return nil, fmt.Errorf("agent: account %q is not on the allowlist", principal.AccountID)
	}
	return client, nil
}
