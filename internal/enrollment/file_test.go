package enrollment_test

import (
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/enrollment"
)

const caPEM = `-----BEGIN CERTIFICATE-----
MIIBkTCCATegAwIBAgIUWnJ3C7dsyDNF9oRlsVJfqqH3s20wCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----
`

func identity() enrollment.Identity {
	return enrollment.Identity{
		ClusterName: "prod-ap-2", ConnectionUID: "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d", Generation: 1, InstallationID: "prod",
		Namespaces: []string{"storefront", "billing"},
		Accounts: []enrollment.Account{
			{ID: "sam", Roles: []string{"viewer"}},
			{ID: "alex", Roles: []string{"viewer", "approver"}},
		},
		ApproverClusterRole: "ptah-operator-ptah-operator-approver",
		ConsoleServerName:   "agents.console.example.com", ConsoleCASHA256: enrollment.CASHA256(caPEM),
	}
}

func file() *enrollment.File {
	return &enrollment.File{
		APIVersion: enrollment.APIVersion, Kind: enrollment.Kind,
		Console:   enrollment.Console{Address: "https://agents.console.example.com", ServerName: "agents.console.example.com", CABundle: caPEM},
		Identity:  identity(),
		Chart:     enrollment.Chart{Reference: "oci://ghcr.io/stokaro/charts/ptah-console-agent", Version: "0.2.0"},
		Bootstrap: "Zm9yLXRlc3RzLW9ubHk", ExpiresAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
		Fingerprint: identity().Fingerprint(),
	}
}

// The fingerprint covers the identity and not the order it is listed in.
func TestTheFingerprintCoversTheIdentityNotItsOrder(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	base := identity().Fingerprint()
	reordered := identity()
	reordered.Namespaces = []string{"billing", "storefront"}
	reordered.Accounts = []enrollment.Account{
		{ID: "alex", Roles: []string{"approver", "viewer"}},
		{ID: "sam", Roles: []string{"viewer"}},
	}

	c.Assert(base, qt.Matches, `sha256:[0-9a-f]{64}`)
	c.Assert(reordered.Fingerprint(), qt.Equals, base)
	for name, change := range map[string]func(*enrollment.Identity){
		"another cluster name":  func(i *enrollment.Identity) { i.ClusterName = "prod-ap-3" },
		"another connection":    func(i *enrollment.Identity) { i.ConnectionUID = "1b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d" },
		"a wider ceiling":       func(i *enrollment.Identity) { i.Namespaces = append(i.Namespaces, "payments") },
		"another role":          func(i *enrollment.Identity) { i.Accounts[0].Roles = []string{"approver"} },
		"another account":       func(i *enrollment.Identity) { i.Accounts[0].ID = "kim" },
		"another approver role": func(i *enrollment.Identity) { i.ApproverClusterRole = "edit-ptah" },
		"another console name":  func(i *enrollment.Identity) { i.ConsoleServerName = "console.example.com" },
		"another console CA":    func(i *enrollment.Identity) { i.ConsoleCASHA256 = enrollment.CASHA256("other") },
		"another installation":  func(i *enrollment.Identity) { i.InstallationID = "staging" },
		"another generation":    func(i *enrollment.Identity) { i.Generation = 2 },
	} {
		changed := identity()
		change(&changed)
		c.Assert(changed.Fingerprint(), qt.Not(qt.Equals), base, qt.Commentf("%s", name))
	}
}

// A file the console wrote reads back whole.
func TestAFileReadsBack(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	encoded, err := file().Marshal()
	c.Assert(err, qt.IsNil)

	parsed, err := enrollment.Parse(encoded)

	c.Assert(err, qt.IsNil)
	c.Assert(parsed, qt.DeepEquals, file())
}

// A file changed on its way to the target is refused.
func TestEachFileRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*enrollment.File)
		says   string
	}{
		{name: "a wider ceiling", change: func(f *enrollment.File) { f.Identity.Namespaces = append(f.Identity.Namespaces, "kube-system") },
			says: `fingerprint: the file does not match what the console issued`},
		{name: "another role", change: func(f *enrollment.File) { f.Identity.Accounts[1].Roles = []string{"approver", "desired-state-author"} },
			says: `fingerprint: the file does not match what the console issued`},
		{name: "another console CA", change: func(f *enrollment.File) { f.Console.CABundle = strings.Replace(caPEM, "MIIB", "MIIC", 1) },
			says: `console: the CA or name is not the one the enrollment names`},
		{name: "another console name", change: func(f *enrollment.File) { f.Console.ServerName = "evil.example.com" },
			says: `console: the CA or name is not the one the enrollment names`},
		{name: "no bootstrap", change: func(f *enrollment.File) { f.Bootstrap = "" },
			says: `the file lacks the bootstrap secret`},
		{name: "another kind", change: func(f *enrollment.File) { f.Kind = "AgentConfiguration" },
			says: `kind: must be`},
		{name: "a generation past the first", change: func(f *enrollment.File) { f.Identity.Generation = 2 },
			says: `identity: a new connection has a lowercase UUID and generation 1`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			changed := file()
			test.change(changed)
			encoded, err := changed.Marshal()
			c.Assert(err, qt.IsNil)

			_, err = enrollment.Parse(encoded)

			c.Assert(err, qt.ErrorMatches, `(?s).*`+test.says+`.*`)
		})
	}
}
