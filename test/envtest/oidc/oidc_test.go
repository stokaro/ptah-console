// Package oidc_test signs people in to a hosted console with an OpenID
// Connect provider that a real kube-apiserver trusts through its
// AuthenticationConfiguration, and shows that the API server, not the
// console, decides who each person is and what they may do.
package oidc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stokaro/ptah-console/internal/auth/oidc/oidctest"
	"github.com/stokaro/ptah-console/internal/hosted"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var (
	plane *envtestenv.Plane
	// trusted is the provider the API server trusts for ptah-console.
	trusted *oidctest.Provider
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	var err error
	trusted, err = oidctest.New("ptah-console")
	if err != nil {
		fmt.Fprintln(os.Stderr, "oidc:", err)
		return 1
	}
	defer trusted.Server.Close()
	dir, err := os.MkdirTemp("", "ptah-console-oidc-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "oidc:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "authentication.yaml")
	if err := os.WriteFile(path, []byte(authentication(trusted)), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "oidc:", err)
		return 1
	}
	return envtestenv.Run(m, envtestenv.Options{
		OperatorCRDs:   true,
		APIServerFlags: map[string]string{"authentication-config": path},
	}, &plane)
}

// authentication is the API server's trust in issuer: its tokens for the
// ptah-console audience name the user oidc:<sub>.
func authentication(issuer *oidctest.Provider) string {
	return `apiVersion: apiserver.config.k8s.io/v1
kind: AuthenticationConfiguration
jwt:
- issuer:
    url: ` + issuer.URL() + `
    audiences: [ptah-console]
    certificateAuthority: |
` + indent(issuer.CA(), "      ") + `  claimMappings:
    username:
      claim: sub
      prefix: "oidc:"
`
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return prefix + strings.Join(lines, "\n"+prefix) + "\n"
}

// console runs a hosted console that signs people in with issuer, against
// the API server, and returns its address.
func console(t *testing.T, issuer *oidctest.Provider, namespaces ...string) string {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "configuration.yaml")
	c.Assert(os.WriteFile(configPath, []byte(`apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: envtest
publicURL: https://console.envtest
namespaces: [`+strings.Join(namespaces, ", ")+`]
oidc:
  issuerURL: `+issuer.URL()+`
  clientID: ptah-console
  displayName: Envtest SSO
  issuerCA: |
`+indent(issuer.CA(), "    ")), 0o600), qt.IsNil)
	// The console's own identity: the factory keeps only the API server's
	// address and CA from it.
	own, err := plane.User("ptah-console-oidc")
	c.Assert(err, qt.IsNil)
	listening := make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- hosted.Run(ctx, hosted.Options{
			ConfigPath: configPath, Listen: "127.0.0.1:0", TLSTerminatedUpstream: true, Kube: own,
			Assets:    fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
			Listening: func(addr net.Addr) { listening <- addr },
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	select {
	case addr := <-listening:
		return addr.String()
	case err := <-done:
		c.Fatalf("the console did not start: %v", err)
		return ""
	}
}

// browser is one person's browser: it keeps the console's cookies and
// follows no redirect on its own.
type browser struct {
	address string
	cookies map[string]*http.Cookie
}

func (b *browser) visit(t *testing.T, path string) *http.Response {
	t.Helper()
	c := qt.New(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+b.address+path, nil)
	c.Assert(err, qt.IsNil)
	request.Host = "console.envtest"
	b.send(request)
	response, err := client.Do(request)
	c.Assert(err, qt.IsNil)
	_ = response.Body.Close()
	b.keep(response)
	return response
}

func (b *browser) call(t *testing.T, procedure, body string) (int, map[string]any) {
	t.Helper()
	c := qt.New(t)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+b.address+"/api/ptah.console.v1."+procedure, strings.NewReader(body))
	c.Assert(err, qt.IsNil)
	request.Host = "console.envtest"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	b.send(request)
	response, err := http.DefaultClient.Do(request)
	c.Assert(err, qt.IsNil)
	defer func() { _ = response.Body.Close() }()
	b.keep(response)
	var answer map[string]any
	c.Assert(json.NewDecoder(response.Body).Decode(&answer), qt.IsNil)
	return response.StatusCode, answer
}

func (b *browser) send(request *http.Request) {
	for _, cookie := range b.cookies {
		request.AddCookie(cookie)
	}
}

func (b *browser) keep(response *http.Response) {
	for _, cookie := range response.Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
			continue
		}
		b.cookies[cookie.Name] = cookie
	}
}

// signIn goes to the provider as person and back, and returns where the
// console sent the browser at the end.
func signIn(t *testing.T, b *browser, issuer *oidctest.Provider, person oidctest.Person) string {
	t.Helper()
	c := qt.New(t)
	started := b.visit(t, "/auth/oidc/login")
	c.Assert(started.StatusCode, qt.Equals, http.StatusFound)
	back, err := issuer.Approve(started.Header.Get("Location"), person)
	c.Assert(err, qt.IsNil)
	callback, err := url.Parse(back)
	c.Assert(err, qt.IsNil)
	finished := b.visit(t, callback.RequestURI())
	c.Assert(finished.StatusCode, qt.Equals, http.StatusSeeOther)
	return finished.Header.Get("Location")
}

// grant lets user list and get schemas in namespace, through a Role of the
// cluster's own; the console takes no part in it.
func grant(t *testing.T, namespace, user string) {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "schema-reader"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.ptah.run"}, Resources: []string{"ptahschemas"}, Verbs: []string{"get", "list"},
		}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "schema-reader"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "schema-reader"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: user}},
	}), qt.IsNil)
}

// A person who signs in with the provider acts as the user the API server
// maps the token to, and RBAC decides what they may read: the namespace
// bound to that user lists, the other is forbidden.
func TestAPersonSignedInWithTheProviderActsAsThemselves(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	grant(t, "oidc-storefront", "oidc:subject-alex")
	c.Assert(plane.Admin.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "oidc-billing"}}), qt.IsNil)
	b := &browser{address: console(t, trusted, "oidc-storefront", "oidc-billing"), cookies: make(map[string]*http.Cookie)}

	landed := signIn(t, b, trusted, oidctest.Person{Subject: "subject-alex", Email: "alex@example.com", Name: "Alex Morgan"})

	c.Assert(landed, qt.Equals, "/")
	c.Assert(b.cookies["__Host-ptah_console"], qt.IsNotNil)
	status, session := b.call(t, "SessionService/GetSession", "{}")
	c.Assert(status, qt.Equals, http.StatusOK, qt.Commentf("%v", session))
	c.Assert(session["kubernetesUsername"], qt.Equals, "oidc:subject-alex")
	c.Assert(session["kubernetesGroups"], qt.DeepEquals, []any{"system:authenticated"})
	c.Assert(session["displayName"], qt.Equals, "Alex Morgan")
	c.Assert(session["signIn"], qt.Equals, "SIGN_IN_OIDC")
	status, listed := b.call(t, "SchemaService/ListSchemas", `{"namespaces":["oidc-storefront","oidc-billing"]}`)
	c.Assert(status, qt.Equals, http.StatusOK, qt.Commentf("%v", listed))
	access := make(map[string]any)
	for _, result := range listed["namespaces"].([]any) {
		row := result.(map[string]any)
		access[row["namespace"].(string)] = row["access"]
	}
	c.Assert(access, qt.DeepEquals, map[string]any{"oidc-storefront": "ACCESS_OK", "oidc-billing": "ACCESS_FORBIDDEN"})
}

// A token from a provider the API server does not trust signs nobody in,
// though the console checked it and found it sound: the API server decides.
func TestATokenTheAPIServerDoesNotTrustSignsNobodyIn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	untrusted, err := oidctest.New("ptah-console")
	c.Assert(err, qt.IsNil)
	t.Cleanup(untrusted.Server.Close)
	b := &browser{address: console(t, untrusted, "oidc-elsewhere"), cookies: make(map[string]*http.Cookie)}

	landed := signIn(t, b, untrusted, oidctest.Person{Subject: "subject-alex"})

	c.Assert(landed, qt.Equals, "/login?error=refused")
	c.Assert(b.cookies["__Host-ptah_console"], qt.IsNil)
}

// Signing out ends the session, and the cookie opens nothing after.
func TestSigningOutEndsTheSignIn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	b := &browser{address: console(t, trusted, "oidc-signout"), cookies: make(map[string]*http.Cookie)}
	c.Assert(signIn(t, b, trusted, oidctest.Person{Subject: "subject-sam"}), qt.Equals, "/")
	kept := b.cookies["__Host-ptah_console"]

	status, _ := b.call(t, "SessionService/Logout", "{}")
	c.Assert(status, qt.Equals, http.StatusOK)
	b.cookies["__Host-ptah_console"] = kept
	status, after := b.call(t, "SessionService/GetSession", "{}")

	c.Assert(status, qt.Equals, http.StatusUnauthorized)
	c.Assert(after["code"], qt.Equals, "unauthenticated")
}
