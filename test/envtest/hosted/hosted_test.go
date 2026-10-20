// Package hosted_test holds the hosted console's clients, and a signed-in
// session, to the API server's verdict. The console acts as a user the API server authenticates by
// certificate, standing in for its ServiceAccount, with the one grant the
// chart gives that ServiceAccount: impersonate on users, limited by
// resourceNames to the accounts' usernames.
package hosted_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/hosted"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{}, &plane))
}

const (
	consoleUser   = "ptah-console-envtest"
	alexUsername  = "ptah-console:envtest:alex"
	samUsername   = "ptah-console:envtest:sam"
	impersonation = "ptah-console-envtest-impersonate"
	namespaces    = "ptah-console-envtest-namespaces"
)

// console is the console's own configuration, once its grants exist: the
// impersonation the chart gives it, and a right of its own no account has.
var console = sync.OnceValues(func() (*rest.Config, error) {
	ctx := context.Background()
	objects := []client.Object{
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: impersonation},
			Rules: []rbacv1.PolicyRule{{
				APIGroups:     []string{""},
				Resources:     []string{"users"},
				Verbs:         []string{"impersonate"},
				ResourceNames: []string{alexUsername, samUsername},
			}},
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: namespaces},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get"}}},
		},
	}
	for _, role := range []string{impersonation, namespaces} {
		objects = append(objects, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: role},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
		})
	}
	for _, object := range objects {
		if err := plane.Admin.Create(ctx, object); err != nil {
			return nil, err
		}
	}
	return plane.User(consoleUser)
})

func consoleConfig(t *testing.T) *rest.Config {
	t.Helper()
	config, err := console()
	qt.New(t).Assert(err, qt.IsNil)
	return config
}

func hostedClient(t *testing.T, account string) *kube.Client {
	t.Helper()
	c := qt.New(t)
	factory, err := kube.NewHostedFactory(consoleConfig(t), map[string]string{
		"alex": alexUsername,
		"sam":  samUsername,
	}, 1)
	c.Assert(err, qt.IsNil)
	accountClient, err := factory.For(kube.Principal{
		Mode: kube.ModeHosted, AccountID: account, Context: kube.HostedConnection, Generation: 1,
	})
	c.Assert(err, qt.IsNil)
	return accountClient
}

// Two accounts used at the same time each act as their own username, with
// no group the console added: the API server adds system:authenticated to
// every impersonated user, and that is all.
func TestTwoAccountsUsedTogetherEachActAsThemselves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		account, username string
	}{
		{account: "alex", username: alexUsername},
		{account: "sam", username: samUsername},
	}
	for _, test := range tests {
		t.Run(test.account, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			hosted := hostedClient(t, test.account)
			seen := make(map[string]int)
			groups := make(map[string]int)

			for range 20 {
				identity, err := kube.WhoAmI(t.Context(), hosted)
				c.Assert(err, qt.IsNil)
				seen[identity.Username]++
				for _, group := range identity.Groups {
					groups[group]++
				}
			}

			c.Assert(seen, qt.DeepEquals, map[string]int{test.username: 20})
			c.Assert(groups, qt.DeepEquals, map[string]int{"system:authenticated": 20})
		})
	}
}

// What the console may do itself, an account may not: the account's client
// is the account, with nothing of the ServiceAccount's rights.
func TestAnAccountHasNoneOfTheConsolesOwnRights(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	itself, err := kube.NewClient(consoleConfig(t))
	c.Assert(err, qt.IsNil)
	alex := hostedClient(t, "alex")

	c.Assert(itself.Get(t.Context(), client.ObjectKey{Name: "default"}, &corev1.Namespace{}), qt.IsNil)
	err = alex.Get(t.Context(), client.ObjectKey{Name: "default"}, &corev1.Namespace{})
	c.Assert(apierrors.IsForbidden(err), qt.IsTrue, qt.Commentf("error: %v", err))
}

// The grant names users and nothing else, so the API server refuses every
// other identity the console's configuration could be pointed at.
func TestTheGrantImpersonatesExactlyTheAccounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		impersonation rest.ImpersonationConfig
		forbidden     bool
	}{
		{name: "an account", impersonation: rest.ImpersonationConfig{UserName: alexUsername}},
		{name: "a username no account has",
			impersonation: rest.ImpersonationConfig{UserName: "ptah-console:envtest:kim"}, forbidden: true},
		{name: "system:masters",
			impersonation: rest.ImpersonationConfig{UserName: "admin", Groups: []string{"system:masters"}}, forbidden: true},
		{name: "a ServiceAccount",
			impersonation: rest.ImpersonationConfig{UserName: "system:serviceaccount:kube-system:default"}, forbidden: true},
		{name: "an account with a group",
			impersonation: rest.ImpersonationConfig{UserName: alexUsername, Groups: []string{"ops"}}, forbidden: true},
		{name: "an account with a UID",
			impersonation: rest.ImpersonationConfig{UserName: alexUsername, UID: "1234"}, forbidden: true},
		{name: "an account with an extra",
			impersonation: rest.ImpersonationConfig{UserName: alexUsername, Extra: map[string][]string{"scopes": {"all"}}},
			forbidden:     true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			config := rest.CopyConfig(consoleConfig(t))
			config.Impersonate = test.impersonation
			impersonating, err := kube.NewClient(config)
			c.Assert(err, qt.IsNil)

			_, err = kube.WhoAmI(t.Context(), impersonating)

			c.Assert(apierrors.IsForbidden(err), qt.Equals, test.forbidden, qt.Commentf("error: %v", err))
		})
	}
}

// signedIn runs a hosted console behind a declared TLS proxy, as the
// console's identity, signs alex in, and returns the console's address and
// the session cookie.
func signedIn(t *testing.T) (address string, cookie *http.Cookie) {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	hash, err := accounts.Hash([]byte("alex-secret"), accounts.MinCost)
	c.Assert(err, qt.IsNil)
	configPath, passwordsPath := filepath.Join(dir, "configuration.yaml"), filepath.Join(dir, "passwords")
	c.Assert(os.WriteFile(configPath, []byte(`apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: envtest
publicURL: https://console.envtest
namespaces: [storefront]
accounts:
  - id: alex
    login: alex@example.com
    displayName: Alex Morgan
`), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(passwordsPath, []byte("alex:"+string(hash)+"\n"), 0o600), qt.IsNil)

	listening := make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- hosted.Run(ctx, hosted.Options{
			ConfigPath: configPath, PasswordsPath: passwordsPath, Listen: "127.0.0.1:0",
			TLSTerminatedUpstream: true, Kube: consoleConfig(t),
			Assets:    fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
			Listening: func(addr net.Addr) { listening <- addr },
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	address = (<-listening).String()
	response := callConsole(t, address, "SessionService/Login", `{"login":"alex@example.com","password":"alex-secret"}`)
	c.Assert(response.StatusCode, qt.Equals, http.StatusOK)
	c.Assert(response.Cookies(), qt.HasLen, 1)
	return address, response.Cookies()[0]
}

func callConsole(t *testing.T, address, procedure, body string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	c := qt.New(t)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+address+"/api/ptah.console.v1."+procedure, strings.NewReader(body))
	c.Assert(err, qt.IsNil)
	request.Host = "console.envtest"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := http.DefaultClient.Do(request)
	c.Assert(err, qt.IsNil)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

// A person who signs in acts as the account's username, which is what the
// API server says when the console asks who the session is.
func TestASignedInAccountActsAsItsUsername(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	address, cookie := signedIn(t)

	response := callConsole(t, address, "SessionService/GetSession", "{}", cookie)

	c.Assert(response.StatusCode, qt.Equals, http.StatusOK)
	var session struct {
		Mode               string   `json:"mode"`
		Context            string   `json:"context"`
		KubernetesUsername string   `json:"kubernetesUsername"`
		KubernetesGroups   []string `json:"kubernetesGroups"`
		AccountID          string   `json:"accountId"`
		DisplayName        string   `json:"displayName"`
	}
	c.Assert(json.NewDecoder(response.Body).Decode(&session), qt.IsNil)
	c.Assert(session.Mode, qt.Equals, "MODE_HOSTED")
	c.Assert(session.Context, qt.Equals, kube.HostedConnection)
	c.Assert(session.KubernetesUsername, qt.Equals, alexUsername)
	c.Assert(session.KubernetesGroups, qt.DeepEquals, []string{"system:authenticated"})
	c.Assert(session.AccountID, qt.Equals, "alex")
	c.Assert(session.DisplayName, qt.Equals, "Alex Morgan")
}
