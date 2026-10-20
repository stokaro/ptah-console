package kube_test

import (
	"os"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/kube"
)

var hostedUsernames = map[string]string{
	"alex": "ptah-console:prod:alex",
	"sam":  "ptah-console:prod:sam",
}

func hostedPrincipal(account string) kube.Principal {
	return kube.Principal{Mode: kube.ModeHosted, AccountID: account, Context: kube.HostedConnection, Generation: 3}
}

// Each account's client impersonates its own username and nothing else, from
// a copy of the console's configuration that is left as it was.
func TestEachAccountImpersonatesItsUsernameAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		account, username string
	}{
		{account: "alex", username: "ptah-console:prod:alex"},
		{account: "sam", username: "ptah-console:prod:sam"},
	}
	for _, test := range tests {
		t.Run(test.account, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			token := filepath.Join(t.TempDir(), "token")
			c.Assert(os.WriteFile(token, []byte("console-service-account"), 0o600), qt.IsNil)
			base := &rest.Config{Host: "https://10.96.0.1", BearerTokenFile: token}
			factory, err := kube.NewHostedFactory(base, hostedUsernames, 3)
			c.Assert(err, qt.IsNil)

			client, err := factory.For(hostedPrincipal(test.account))

			c.Assert(err, qt.IsNil)
			c.Assert(client.Config.Impersonate, qt.DeepEquals, rest.ImpersonationConfig{UserName: test.username})
			c.Assert(client.Config.BearerTokenFile, qt.Equals, token)
			c.Assert(base.Impersonate, qt.DeepEquals, rest.ImpersonationConfig{})
		})
	}
}

func TestTheHostedFactoryRefusesAnyoneElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		principal kube.Principal
		says      string
	}{
		{name: "an account it does not have", principal: hostedPrincipal("kim"),
			says: `kube: account "kim" is not in this console's configuration`},
		{name: "a local principal", principal: kube.Principal{Mode: kube.ModeLocal, Context: kube.HostedConnection, Generation: 3},
			says: "kube: a hosted console cannot act for a local principal"},
		{name: "another connection", principal: kube.Principal{Mode: kube.ModeHosted, AccountID: "alex", Context: "prod-ap-2", Generation: 3},
			says: `kube: a hosted console acts through "in-cluster", not "prod-ap-2"`},
		{name: "an earlier configuration", principal: kube.Principal{Mode: kube.ModeHosted, AccountID: "alex", Context: kube.HostedConnection, Generation: 2},
			says: "kube: the configuration is generation 3, not 2"},
		{name: "no account", principal: kube.Principal{Mode: kube.ModeHosted, Context: kube.HostedConnection, Generation: 3},
			says: "kube: a hosted principal needs a Console account"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			factory, err := kube.NewHostedFactory(&rest.Config{Host: "https://10.96.0.1"}, hostedUsernames, 3)
			c.Assert(err, qt.IsNil)

			client, err := factory.For(test.principal)

			c.Assert(client, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

func TestAHostedFactoryIsNotBuiltFromWhatItCannotHold(t *testing.T) {
	t.Parallel()
	plain := &rest.Config{Host: "https://10.96.0.1"}
	tests := []struct {
		name      string
		base      *rest.Config
		usernames map[string]string
		says      string
	}{
		{name: "no configuration", base: nil, usernames: hostedUsernames,
			says: "kube: the console's own configuration is required"},
		{name: "a configuration that impersonates a user",
			base:      &rest.Config{Host: "https://10.96.0.1", Impersonate: rest.ImpersonationConfig{UserName: "admin"}},
			usernames: hostedUsernames, says: "kube: the console's own configuration already impersonates someone"},
		{name: "a configuration that impersonates a group",
			base:      &rest.Config{Host: "https://10.96.0.1", Impersonate: rest.ImpersonationConfig{Groups: []string{"system:masters"}}},
			usernames: hostedUsernames, says: "kube: the console's own configuration already impersonates someone"},
		{name: "no account", base: plain, usernames: make(map[string]string),
			says: "kube: a hosted console needs at least one account"},
		{name: "an account without an ID", base: plain, usernames: map[string]string{"": "ptah-console:prod:x"},
			says: "kube: an account needs an ID"},
		{name: "an account without a username", base: plain, usernames: map[string]string{"alex": ""},
			says: `kube: account "alex" has no username`},
		{name: "a system: username", base: plain, usernames: map[string]string{"alex": "system:admin"},
			says: `kube: account "alex" may not act as "system:admin", a name Kubernetes keeps for itself`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			factory, err := kube.NewHostedFactory(test.base, test.usernames, 1)

			c.Assert(factory, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}
