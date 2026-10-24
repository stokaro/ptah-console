package accounts_test

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"sigs.k8s.io/yaml"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

func example(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "configuration.yaml"))
	qt.New(t).Assert(err, qt.IsNil)
	return data
}

func TestEachAccountActsAsAUsernameTheServerBuilds(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	config, err := accounts.ParseConfig(example(t))

	c.Assert(err, qt.IsNil)
	c.Assert(config.InstallationID, qt.Equals, "prod-eu-1")
	c.Assert(config.Namespaces, qt.DeepEquals, []string{"storefront", "billing"})
	c.Assert(config.Accounts, qt.HasLen, 2)
	c.Assert(config.Username(config.Accounts[0]), qt.Equals, "ptah-console:prod-eu-1:alex")
	c.Assert(config.Username(config.Accounts[1]), qt.Equals, "ptah-console:prod-eu-1:sam")
	c.Assert(config.Accounts[0].DisplayName, qt.Equals, "Alex Morgan")
}

// changed is the example with one change, as YAML.
func changed(t *testing.T, change func(*accounts.Config)) string {
	t.Helper()
	c := qt.New(t)
	config, err := accounts.ParseConfig(example(t))
	c.Assert(err, qt.IsNil)
	change(config)
	data, err := yaml.Marshal(config)
	c.Assert(err, qt.IsNil)
	return string(data)
}

func TestEachConfigurationRefusal(t *testing.T) {
	t.Parallel()
	const label = "must be 1 to 63 lowercase letters, digits and hyphens, starting and ending with a letter or digit"
	tests := []struct {
		name   string
		change func(*accounts.Config)
		want   string
	}{
		{name: "another version", change: func(c *accounts.Config) { c.APIVersion = "console.ptah.run/v1" },
			want: "apiVersion: must be console.ptah.run/v1alpha1"},
		{name: "another kind", change: func(c *accounts.Config) { c.Kind = "Config" },
			want: "kind: must be HostedConfiguration"},
		{name: "no installation ID", change: func(c *accounts.Config) { c.InstallationID = "" },
			want: "installationID: " + label},
		{name: "an installation ID with a colon", change: func(c *accounts.Config) { c.InstallationID = "prod:eu" },
			want: "installationID: " + label},
		{name: "no public URL", change: func(c *accounts.Config) { c.PublicURL = "" },
			want: "publicURL: is required"},
		{name: "a public URL that is not one", change: func(c *accounts.Config) { c.PublicURL = "https://[::1" },
			want: "publicURL: is not a URL"},
		{name: "plain HTTP", change: func(c *accounts.Config) { c.PublicURL = "http://console.example.com" },
			want: "publicURL: must be an https URL; a hosted console serves HTTPS only"},
		{name: "no host", change: func(c *accounts.Config) { c.PublicURL = "https:///" },
			want: "publicURL: names no host"},
		{name: "credentials in the URL", change: func(c *accounts.Config) { c.PublicURL = "https://a:b@console.example.com" },
			want: "publicURL: must not carry a user name or password"},
		{name: "a query", change: func(c *accounts.Config) { c.PublicURL = "https://console.example.com/?a=b" },
			want: "publicURL: must not carry a query or a fragment"},
		{name: "a fragment", change: func(c *accounts.Config) { c.PublicURL = "https://console.example.com/#top" },
			want: "publicURL: must not carry a query or a fragment"},
		{name: "a path", change: func(c *accounts.Config) { c.PublicURL = "https://example.com/console" },
			want: "publicURL: must be the root of its host; the console is served at /"},
		{name: "no namespace", change: func(c *accounts.Config) { c.Namespaces = nil },
			want: "namespaces: names no namespace; the console shows only the namespaces listed here"},
		{name: "a namespace that cannot exist", change: func(c *accounts.Config) { c.Namespaces[1] = "Billing" },
			want: "namespaces[1]: " + label},
		{name: "a namespace twice", change: func(c *accounts.Config) { c.Namespaces[1] = "storefront" },
			want: `namespaces[1]: repeats namespaces[0]`},
		{name: "no account", change: func(c *accounts.Config) { c.Accounts = nil },
			want: "accounts: lists no account"},
		{name: "an account without an ID", change: func(c *accounts.Config) { c.Accounts[1].ID = "" },
			want: `accounts[1].id: ` + label},
		{name: "a system: ID", change: func(c *accounts.Config) { c.Accounts[1].ID = "system:masters" },
			want: `accounts[1].id: ` + label},
		{name: "an ID twice", change: func(c *accounts.Config) { c.Accounts[1].ID = "alex" },
			want: `accounts[1].id: repeats accounts[0].id`},
		{name: "an account without a login", change: func(c *accounts.Config) { c.Accounts[1].Login = "" },
			want: `accounts[1].login: is required`},
		{name: "a login with a space", change: func(c *accounts.Config) { c.Accounts[1].Login = "sam smith" },
			want: `accounts[1].login: must be ASCII letters, digits and punctuation, without spaces`},
		{name: "a login past ASCII", change: func(c *accounts.Config) { c.Accounts[1].Login = "sám" },
			want: `accounts[1].login: must be ASCII letters, digits and punctuation, without spaces`},
		{name: "a long login", change: func(c *accounts.Config) { c.Accounts[1].Login = strings.Repeat("s", 255) },
			want: `accounts[1].login: is longer than 254 bytes`},
		{name: "a system: login", change: func(c *accounts.Config) { c.Accounts[1].Login = "System:admin" },
			want: `accounts[1].login: must not start with system:, which Kubernetes keeps for itself`},
		{name: "a login twice, in another case", change: func(c *accounts.Config) { c.Accounts[1].Login = "Alex@Example.com" },
			want: `accounts[1].login: repeats accounts[0].login, ignoring case`},
		{name: "a control character in a name", change: func(c *accounts.Config) { c.Accounts[0].DisplayName = "Alex\x1b[2J" },
			want: `accounts[0].displayName: must not hold control characters`},
		{name: "a long name", change: func(c *accounts.Config) { c.Accounts[0].DisplayName = strings.Repeat("A", 129) },
			want: `accounts[0].displayName: is longer than 128 bytes`},
		{name: "a proxy that is no network", change: func(c *accounts.Config) { c.TrustedProxies = []string{"10.244.0.0"} },
			want: "trustedProxies[0]: is not a network in CIDR notation, such as 10.244.0.0/16 or 192.0.2.10/32"},
		{name: "every IPv4 address as a proxy", change: func(c *accounts.Config) { c.TrustedProxies = []string{"0.0.0.0/0"} },
			want: "trustedProxies[0]: is every address, which would let any client say it is any other"},
		{name: "every IPv6 address as a proxy", change: func(c *accounts.Config) { c.TrustedProxies = []string{"::/0"} },
			want: "trustedProxies[0]: is every address, which would let any client say it is any other"},
		{name: "an IPv4 network written as IPv6", change: func(c *accounts.Config) { c.TrustedProxies = []string{"::ffff:10.244.0.0/112"} },
			want: "trustedProxies[0]: is an IPv4 network written as IPv6; write it as IPv4"},
		{name: "a host address with a network's length", change: func(c *accounts.Config) { c.TrustedProxies = []string{"10.244.1.5/16"} },
			want: "trustedProxies[0]: is not a network address; write 10.244.0.0/16"},
		{name: "a network twice", change: func(c *accounts.Config) { c.TrustedProxies = []string{"10.244.0.0/16", "10.244.0.0/16"} },
			want: "trustedProxies[1]: repeats trustedProxies[0]"},
		{name: "a registry in a namespace that cannot exist", change: func(c *accounts.Config) {
			c.Registry = &accounts.Registry{Namespace: "Ptah", ThisCluster: "this-cluster-3f2c4b1e", ConnectionUID: registryConnection}
		}, want: "registry.namespace: " + label},
		{name: "a registry entry that cannot be an object name", change: func(c *accounts.Config) {
			c.Registry = &accounts.Registry{Namespace: "ptah-console", ThisCluster: "This Cluster", ConnectionUID: registryConnection}
		}, want: "registry.thisCluster: is not the name of an object"},
		{name: "a connection UID that is not a UUID", change: func(c *accounts.Config) {
			c.Registry = &accounts.Registry{Namespace: "ptah-console", ThisCluster: "this-cluster-3f2c4b1e", ConnectionUID: "3F2C4B1E-8D0A-4C2E-9B7F-1A2B3C4D5E6F"}
		}, want: "registry.connectionUID: is not a lowercase UUID"},
		{name: "too many networks", change: func(c *accounts.Config) {
			c.TrustedProxies = make([]string, 65)
			for i := range c.TrustedProxies {
				c.TrustedProxies[i] = fmt.Sprintf("10.%d.0.0/16", i)
			}
		}, want: "trustedProxies: lists more than 64 networks"},
		{name: "more namespaces than the console reads", change: func(c *accounts.Config) {
			c.Namespaces = numberedNamespaces(api.MaxNamespaces + 1)
		}, want: fmt.Sprintf("namespaces: lists more than %d namespaces, the most the console reads", api.MaxNamespaces)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			document := changed(t, test.change)

			config, err := accounts.ParseConfig([]byte(document))

			c.Assert(config, qt.IsNil)
			invalid := new(accounts.InvalidError)
			c.Assert(err, qt.ErrorAs, &invalid)
			c.Assert(invalid.Problems, qt.DeepEquals, []string{test.want}, qt.Commentf("%s", document))
		})
	}
}

const registryConnection = "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f"

func numberedNamespaces(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("team-%02d", i)
	}
	return names
}

// The ceiling may name as many namespaces as the console reads in one
// request, and no more.
func TestTheCeilingMayNameAsManyNamespacesAsTheConsoleReads(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	document := changed(t, func(config *accounts.Config) {
		config.Namespaces = numberedNamespaces(api.MaxNamespaces)
	})

	config, err := accounts.ParseConfig([]byte(document))

	c.Assert(err, qt.IsNil)
	c.Assert(config.Namespaces, qt.HasLen, api.MaxNamespaces)
}

// A configuration that names a registry is read with it, and one that names
// none has none: the console then reaches only the cluster it runs in.
func TestARegistryIsReadWhenTheConfigurationNamesOne(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	document := changed(t, func(config *accounts.Config) {
		config.Registry = &accounts.Registry{Namespace: "ptah-console", ThisCluster: "this-cluster-3f2c4b1e", ConnectionUID: registryConnection}
	})
	config, err := accounts.ParseConfig([]byte(document))
	c.Assert(err, qt.IsNil)
	c.Assert(config.Registry, qt.DeepEquals, &accounts.Registry{
		Namespace: "ptah-console", ThisCluster: "this-cluster-3f2c4b1e", ConnectionUID: registryConnection,
	})
	config, err = accounts.ParseConfig([]byte(changed(t, func(*accounts.Config) {})))
	c.Assert(err, qt.IsNil)
	c.Assert(config.Registry, qt.IsNil)
}

func TestAFileThatIsNotAConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		document string
		want     string
	}{
		{name: "a field nobody reads", document: "apiVersion: console.ptah.run/v1alpha1\npasswrods: x\n",
			want: `refused:\n  - not a configuration: .*unknown field "passwrods".*`},
		{name: "a key given twice", document: "kind: HostedConfiguration\nkind: HostedConfiguration\n",
			want: `(?s)refused:\n  - not a configuration: .*"kind" already set.*`},
		{name: "a list where an object goes", document: "- a\n- b\n",
			want: `refused:\n  - not a configuration: .*cannot unmarshal array.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			config, err := accounts.ParseConfig([]byte(test.document))

			c.Assert(config, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.want)
		})
	}
}

func TestTrustedProxiesAreReadAsNetworks(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	document := changed(t, func(config *accounts.Config) {
		config.TrustedProxies = []string{"10.244.0.0/16", "192.0.2.10/32", "fd00:10::/64"}
	})

	config, err := accounts.ParseConfig([]byte(document))

	c.Assert(err, qt.IsNil)
	c.Assert(fmt.Sprint(config.Proxies()), qt.Equals, fmt.Sprint([]netip.Prefix{
		netip.MustParsePrefix("10.244.0.0/16"), netip.MustParsePrefix("192.0.2.10/32"), netip.MustParsePrefix("fd00:10::/64"),
	}))
	c.Assert(config.Proxies(), qt.HasLen, 3)
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	document := changed(t, func(config *accounts.Config) {
		config.PublicURL = "http://console.example.com"
		config.Accounts[1].ID = "alex"
		config.Accounts[1].Login = "ALEX@example.com"
	})

	_, err := accounts.ParseConfig([]byte(document))

	c.Assert(err, qt.IsNotNil)
	c.Assert(err.Error(), qt.Equals, "refused:\n"+
		"  - publicURL: must be an https URL; a hosted console serves HTTPS only\n"+
		"  - accounts[1].id: repeats accounts[0].id\n"+
		"  - accounts[1].login: repeats accounts[0].login, ignoring case")
}
