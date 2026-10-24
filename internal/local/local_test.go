package local_test

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/local"
	"github.com/stokaro/ptah-console/internal/localstate"
)

// pluginKubeconfig writes a kubeconfig whose only context runs a credential
// plugin. The cluster is never reached: every case here ends before a
// request is sent.
func pluginKubeconfig(t *testing.T) string {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.Clusters["prod"] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:1"}
	config.AuthInfos["sso"] = &clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{
		Command: "kubelogin", Args: []string{"get-token"}, APIVersion: "client.authentication.k8s.io/v1",
		InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
	}}
	config.Contexts["production"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "sso"}
	config.CurrentContext = "production"
	path := filepath.Join(t.TempDir(), "config")
	qt.New(t).Assert(clientcmd.WriteToFile(*config, path), qt.IsNil)
	return path
}

func TestACredentialPluginRunsOnlyOnceAgreedTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		interactive bool
		answer      string
		flag        bool
		refusal     string
		trusted     bool
	}{
		{name: "no terminal and no flag", refusal: ".*runs \"kubelogin get-token\" to get credentials.*--trust-exec-plugin.*"},
		{name: "the person says no", interactive: true, answer: "n\n", refusal: ".*not agreed to.*"},
		{name: "the person presses Enter", interactive: true, answer: "\n", refusal: ".*not agreed to.*"},
		{name: "the person says yes", interactive: true, answer: "y\n", trusted: true},
		{name: "the flag", flag: true, trusted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			state := filepath.Join(t.TempDir(), "state.json")
			ctx, cancel := context.WithCancel(context.Background())
			// A run that gets past the question stops at once.
			cancel()

			err := local.Run(ctx, local.Options{
				Kubeconfig:      pluginKubeconfig(t),
				StatePath:       state,
				TrustExecPlugin: test.flag,
				Interactive:     test.interactive,
				Stdin:           strings.NewReader(test.answer),
				Stdout:          &bytes.Buffer{},
				Assets:          fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
			})
			saved, loadErr := localstate.Load(state)

			c.Assert(err != nil, qt.Equals, test.refusal != "", qt.Commentf("error: %v", err))
			c.Assert(loadErr, qt.IsNil)
			c.Assert(len(saved.TrustedExecPlugins) == 1, qt.Equals, test.trusted)
		})
	}
}

// Namespaces the console could not read in one request are refused before
// the kubeconfig is read or a port is taken, so nothing is cut short later.
func TestNamespacesPastTheBoundAreRefusedAtStart(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	names := make([]string, api.MaxNamespaces+1)
	for i := range names {
		names[i] = fmt.Sprintf("team-%02d", i)
	}

	err := local.Run(t.Context(), local.Options{
		Kubeconfig: filepath.Join(t.TempDir(), "absent"),
		Namespaces: names,
	})

	c.Assert(err, qt.ErrorMatches, fmt.Sprintf("--namespace: %d namespaces named; the console reads at most %d", api.MaxNamespaces+1, api.MaxNamespaces))
}
