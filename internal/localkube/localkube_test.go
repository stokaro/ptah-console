package localkube_test

import (
	"os"
	"path/filepath"
	"testing"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/stokaro/ptah-console/internal/localkube"
)

// kubeconfig writes a file with two clusters and a context for each way of
// authenticating the console has to tell apart.
func kubeconfig(t *testing.T, current string) string {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.Clusters["prod"] = &clientcmdapi.Cluster{Server: "https://prod.example:6443"}
	config.Clusters["lab"] = &clientcmdapi.Cluster{Server: "https://lab.example:6443", InsecureSkipTLSVerify: true}
	config.AuthInfos["token"] = &clientcmdapi.AuthInfo{Token: "t"}
	config.AuthInfos["plugin"] = &clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{
		Command: "kubelogin", Args: []string{"get-token"}, APIVersion: "client.authentication.k8s.io/v1",
		InteractiveMode: clientcmdapi.IfAvailableExecInteractiveMode,
	}}
	config.AuthInfos["provider"] = &clientcmdapi.AuthInfo{AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "oidc"}}
	config.AuthInfos["impersonating"] = &clientcmdapi.AuthInfo{Token: "t", Impersonate: "dba@example.com"}
	config.Contexts["production"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "token"}
	config.Contexts["sso"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "plugin"}
	config.Contexts["legacy"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "provider"}
	config.Contexts["as-dba"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "impersonating"}
	config.Contexts["lab"] = &clientcmdapi.Context{Cluster: "lab", AuthInfo: "token"}
	config.CurrentContext = current
	path := filepath.Join(t.TempDir(), "config")
	qt.New(t).Assert(clientcmd.WriteToFile(*config, path), qt.IsNil)
	return path
}

func TestResolvePinsTheContextItWasGiven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current string
		context string
		pinned  string
		server  string
	}{
		{name: "named", current: "lab", context: "production", pinned: "production", server: "https://prod.example:6443"},
		{name: "the current context", current: "lab", context: "", pinned: "lab", server: "https://lab.example:6443"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			resolved, err := localkube.Resolve(kubeconfig(t, test.current), test.context)

			c.Assert(err, qt.IsNil)
			c.Assert(resolved.Context, qt.Equals, test.pinned)
			c.Assert(resolved.Config.Host, qt.Equals, test.server)
		})
	}
}

func TestResolveRefusesAContextItCannotActWith(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current string
		context string
		refusal string
	}{
		{name: "no current context and none named", current: "", context: "", refusal: ".*sets no current-context.*"},
		{name: "an unknown context", current: "lab", context: "staging", refusal: `.*has no context "staging".*`},
		{name: "a legacy auth-provider", current: "lab", context: "legacy", refusal: `.*"oidc" auth-provider.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			resolved, err := localkube.Resolve(kubeconfig(t, test.current), test.context)

			c.Assert(err, qt.ErrorMatches, test.refusal)
			c.Assert(resolved.Config, qt.IsNil)
		})
	}
}

// The context is resolved once. Editing the kubeconfig's current-context
// afterwards moves nothing that was already resolved.
func TestALaterCurrentContextMovesNothing(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	path := kubeconfig(t, "production")
	resolved, err := localkube.Resolve(path, "")
	c.Assert(err, qt.IsNil)

	edited := kubeconfig(t, "lab")
	contents, err := os.ReadFile(edited)
	c.Assert(err, qt.IsNil)
	c.Assert(os.WriteFile(path, contents, 0o600), qt.IsNil)

	c.Assert(resolved.Context, qt.Equals, "production")
	c.Assert(resolved.Config.Host, qt.Equals, "https://prod.example:6443")
}

func TestResolveSaysWhatTheContextDoes(t *testing.T) {
	t.Parallel()
	path := kubeconfig(t, "production")
	tests := []struct {
		name         string
		context      string
		plugin       string
		impersonates string
		insecure     bool
	}{
		{name: "a token", context: "production"},
		{name: "a credential plugin", context: "sso", plugin: "kubelogin"},
		{name: "impersonation in the kubeconfig", context: "as-dba", impersonates: "dba@example.com"},
		{name: "TLS verification off", context: "lab", insecure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			resolved, err := localkube.Resolve(path, test.context)
			c.Assert(err, qt.IsNil)

			c.Assert(pluginCommand(resolved), qt.Equals, test.plugin)
			c.Assert(resolved.Impersonates, qt.Equals, test.impersonates)
			c.Assert(resolved.InsecureTLS, qt.Equals, test.insecure)
		})
	}
}

func pluginCommand(resolved localkube.Resolved) string {
	if resolved.ExecPlugin == nil {
		return ""
	}
	return resolved.ExecPlugin.Command
}

// The digest is a question about exactly what runs: change the arguments or
// the environment and it is a different plugin.
func TestThePluginDigestChangesWithWhatRuns(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	path := kubeconfig(t, "sso")
	before, err := localkube.Resolve(path, "sso")
	c.Assert(err, qt.IsNil)

	config, err := clientcmd.LoadFromFile(path)
	c.Assert(err, qt.IsNil)
	config.AuthInfos["plugin"].Exec.Args = []string{"get-token", "--server-id", "other"}
	c.Assert(clientcmd.WriteToFile(*config, path), qt.IsNil)
	after, err := localkube.Resolve(path, "sso")
	c.Assert(err, qt.IsNil)

	c.Assert(before.ExecPlugin.Digest, qt.Matches, `sha256:[0-9a-f]{64}`)
	c.Assert(after.ExecPlugin.Digest, qt.Not(qt.Equals), before.ExecPlugin.Digest)
}
