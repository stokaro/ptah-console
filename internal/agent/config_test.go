package agent_test

import (
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/agent"
)

const configuration = `apiVersion: console.ptah.run/v1alpha1
kind: AgentConfiguration
installationID: prod
connection:
  uid: 3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f
  generation: 1
console:
  address: https://agents.console.example.com:8444
  serverName: agents.console.example.com
namespaces: [storefront, billing]
`

const allowlist = `apiVersion: console.ptah.run/v1alpha1
kind: AgentAccounts
accounts: [alex, sam]
`

func TestAConfigurationNamesItsConnectionConsoleAndCeiling(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	config, err := agent.ParseConfiguration([]byte(configuration))

	c.Assert(err, qt.IsNil)
	c.Assert(config.Connection, qt.Equals, agent.Connection{UID: "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f", Generation: 1})
	c.Assert(config.Namespaces, qt.DeepEquals, []string{"storefront", "billing"})
	c.Assert(config.Username("alex"), qt.Equals, "ptah-console:prod:alex")
}

func TestEachConfigurationRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		from, to string
		says     string
	}{
		{name: "another kind", from: "kind: AgentConfiguration", to: "kind: HostedConfiguration", says: `kind: must be`},
		{name: "a field the agent does not read", from: "namespaces:", to: "groups: [system:masters]\nnamespaces:", says: `unknown field "groups"`},
		{name: "an installation ID with a colon", from: "installationID: prod", to: "installationID: 'prod:x'", says: `installationID: must be`},
		{name: "a connection UID in capitals", from: "3f2c4b1e-8d0a", to: "3F2C4B1E-8D0A", says: `connection.uid: is not a lowercase UUID`},
		{name: "generation zero", from: "generation: 1", to: "generation: 0", says: `connection.generation: must be 1 or more`},
		{name: "a console over HTTP", from: "https://agents", to: "http://agents", says: `console.address: must be https`},
		{name: "a console address with a path", from: ":8444", to: ":8444/agents", says: `console.address: must have no path`},
		{name: "a console address with a user", from: "https://agents", to: "https://admin@agents", says: `console.address: must name a host and nothing else`},
		{name: "a server name that is no name", from: "serverName: agents.console.example.com", to: "serverName: '*.example.com'", says: `console.serverName: is not a DNS name`},
		{name: "no namespace", from: "[storefront, billing]", to: "[]", says: `namespaces: names none`},
		{name: "a namespace twice", from: "[storefront, billing]", to: "[storefront, storefront]", says: `namespaces\[1\]: repeats namespaces\[0\]`},
		{name: "a wildcard namespace", from: "[storefront, billing]", to: "['*']", says: `namespaces\[0\]: must be`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			c.Assert(configuration, qt.Contains, test.from)

			_, err := agent.ParseConfiguration([]byte(strings.Replace(configuration, test.from, test.to, 1)))

			c.Assert(err, qt.ErrorMatches, `(?s).*`+test.says+`.*`)
		})
	}
}

func TestEachAllowlistRefusal(t *testing.T) {
	t.Parallel()
	_, err := agent.ParseAccounts([]byte(allowlist))
	qt.New(t).Assert(err, qt.IsNil)
	tests := []struct {
		name     string
		from, to string
		says     string
	}{
		{name: "another kind", from: "kind: AgentAccounts", to: "kind: AgentConfiguration", says: `kind: must be`},
		{name: "no account", from: "[alex, sam]", to: "[]", says: `accounts: names none`},
		{name: "an account twice", from: "[alex, sam]", to: "[alex, alex]", says: `accounts\[1\]: repeats accounts\[0\]`},
		{name: "a username as an account", from: "[alex, sam]", to: "['system:masters']", says: `accounts\[0\]: must be`},
		{name: "a group beside the accounts", from: "accounts:", to: "groups: [system:masters]\naccounts:", says: `unknown field "groups"`},
		{name: "usernames beside the accounts", from: "accounts:", to: "usernames: {alex: admin}\naccounts:", says: `unknown field "usernames"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			c.Assert(allowlist, qt.Contains, test.from)

			_, err := agent.ParseAccounts([]byte(strings.Replace(allowlist, test.from, test.to, 1)))

			c.Assert(err, qt.ErrorMatches, `(?s).*`+test.says+`.*`)
		})
	}
}
