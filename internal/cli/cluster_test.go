package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/cli"
	"github.com/stokaro/ptah-console/internal/enrollment"
)

const consoleCA = `-----BEGIN CERTIFICATE-----
MIIBkTCCATegAwIBAgIUWnJ3C7dsyDNF9oRlsVJfqqH3s20wCgYIKoZIzj0EAwIw
-----END CERTIFICATE-----
`

// enrollmentFile writes a whole enrollment file, changed by change, and
// returns its path.
func enrollmentFile(t *testing.T, change func(*enrollment.File)) string {
	t.Helper()
	c := qt.New(t)
	identity := enrollment.Identity{
		ClusterName: "prod-ap-2", ConnectionUID: "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d", Generation: 1, InstallationID: "prod",
		Namespaces: []string{"storefront"}, Accounts: []enrollment.Account{{ID: "alex", Roles: []string{"viewer"}}},
		ApproverClusterRole: "ptah-operator-ptah-operator-approver",
		ConsoleServerName:   "agents.console.example.com", ConsoleCASHA256: enrollment.CASHA256(consoleCA),
	}
	file := &enrollment.File{
		APIVersion: enrollment.APIVersion, Kind: enrollment.Kind,
		Console:  enrollment.Console{Address: "https://agents.console.example.com", ServerName: "agents.console.example.com", CABundle: consoleCA},
		Identity: identity, Chart: enrollment.Chart{Reference: "oci://ghcr.io/stokaro/charts/ptah-console-agent", Version: "0.2.0"},
		Bootstrap: "Zm9yLXRlc3RzLW9ubHk", ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		Fingerprint: identity.Fingerprint(),
	}
	change(file)
	encoded, err := file.Marshal()
	c.Assert(err, qt.IsNil)
	path := filepath.Join(t.TempDir(), "enrollment.yaml")
	c.Assert(os.WriteFile(path, encoded, 0o600), qt.IsNil)
	return path
}

// kubeconfig is a kubeconfig with one context, enroll-me, for a server that
// is not there.
func kubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	qt.New(t).Assert(os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
current-context: enroll-me
clusters: [{name: target, cluster: {server: "https://127.0.0.1:1", certificate-authority-data: "`+
		strings.TrimSpace(base64CA)+`"}}]
users: [{name: admin, user: {token: not-a-token}}]
contexts: [{name: enroll-me, context: {cluster: target, user: admin}}]
`), 0o600), qt.IsNil)
	return path
}

const base64CA = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJrVENDQVRlZ0F3SUJBZ0lVV25KM0M3ZHN5RE5GOW9SbHNWSmZxcUgzczIwd0NnWUlLb1pJemowRUF3SXcKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="

// enroll refuses what it cannot run safely, before it reaches any cluster.
func TestEnrollRefusesWhatItCannotRunSafely(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args func(t *testing.T) []string
		code int
		says string
	}{
		{name: "no context, though the kubeconfig has a current one", args: func(t *testing.T) []string {
			return []string{"cluster", "enroll", "--kubeconfig", kubeconfig(t), "--enrollment-file", enrollmentFile(t, func(*enrollment.File) {})}
		}, code: cli.ExitUsage, says: `--context is required: enroll never uses the kubeconfig's current context`},
		{name: "no enrollment file", args: func(t *testing.T) []string {
			return []string{"cluster", "enroll", "--kubeconfig", kubeconfig(t), "--context", "enroll-me"}
		}, code: cli.ExitUsage, says: `--enrollment-file is required`},
		{name: "a file widened on its way", args: func(t *testing.T) []string {
			return []string{"cluster", "enroll", "--kubeconfig", kubeconfig(t), "--context", "enroll-me", "--enrollment-file",
				enrollmentFile(t, func(f *enrollment.File) { f.Identity.Namespaces = append(f.Identity.Namespaces, "kube-system") })}
		}, code: cli.ExitFailed, says: `fingerprint: the file does not match what the console issued`},
		{name: "a context the kubeconfig does not have", args: func(t *testing.T) []string {
			return []string{"cluster", "enroll", "--kubeconfig", kubeconfig(t), "--context", "other", "--enrollment-file", enrollmentFile(t, func(*enrollment.File) {})}
		}, code: cli.ExitFailed, says: `the kubeconfig has no context "other"`},
		{name: "an expired file", args: func(t *testing.T) []string {
			return []string{"cluster", "enroll", "--kubeconfig", kubeconfig(t), "--context", "enroll-me", "--enrollment-file",
				enrollmentFile(t, func(f *enrollment.File) { f.ExpiresAt = time.Now().Add(-time.Minute).UTC().Truncate(time.Second) })}
		}, code: cli.ExitFailed, says: `the file expired at .*; start a new enrollment in the console`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, _, stderr := run(test.args(t)...)

			c.Assert(code, qt.Equals, test.code, qt.Commentf("%s", stderr))
			c.Assert(stderr, qt.Matches, `(?s).*`+test.says+`.*`)
		})
	}
}
