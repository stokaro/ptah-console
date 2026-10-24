package hosted_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/auth/oidc/oidctest"
	"github.com/stokaro/ptah-console/internal/hosted"
)

// oidcConfiguration signs people in with issuer instead of accounts.
func oidcConfiguration(issuer *oidctest.Provider) string {
	return `apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: test
publicURL: https://console.example.test
namespaces: [storefront]
oidc:
  issuerURL: ` + issuer.URL() + `
  clientID: ptah-console
  displayName: Example SSO
  issuerCA: |
` + indent(issuer.CA(), "    ")
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return prefix + strings.Join(lines, "\n"+prefix) + "\n"
}

// startOIDC runs a hosted console that signs people in with issuer. Its own
// Kubernetes identity points nowhere, so the API server never answers.
func startOIDC(t *testing.T, issuer *oidctest.Provider) *console {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "configuration.yaml")
	c.Assert(os.WriteFile(configPath, []byte(oidcConfiguration(issuer)), 0o600), qt.IsNil)
	served := &console{roots: x509.NewCertPool(), certFile: filepath.Join(dir, "tls.crt"), keyFile: filepath.Join(dir, "tls.key")}
	served.roots.AddCert(certificate(t, served.certFile, served.keyFile, 1))
	listening := make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- hosted.Run(ctx, hosted.Options{
			ConfigPath: configPath, Listen: "127.0.0.1:0",
			TLSCertFile: served.certFile, TLSKeyFile: served.keyFile,
			Kube: &rest.Config{Host: "https://127.0.0.1:1"}, Assets: assets,
			Listening: func(addr net.Addr) { listening <- addr },
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	select {
	case addr := <-listening:
		served.address = addr.String()
	case err := <-done:
		c.Fatalf("the console did not start: %v", err)
	}
	return served
}

func newIssuer(t *testing.T) *oidctest.Provider {
	t.Helper()
	issuer, err := oidctest.New("ptah-console")
	qt.New(t).Assert(err, qt.IsNil)
	t.Cleanup(issuer.Server.Close)
	return issuer
}

// visit is one navigation the browser makes, without following the
// redirect it answers with.
func (s *console) visit(t *testing.T, path string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	c := qt.New(t)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{RootCAs: s.roots, ServerName: publicHost, MinVersion: tls.VersionTLS12},
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+s.address+path, nil)
	c.Assert(err, qt.IsNil)
	request.Host = publicHost
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := client.Do(request)
	c.Assert(err, qt.IsNil)
	_ = response.Body.Close()
	return response
}

// pathOf is the path and query a callback URL names on the console.
func pathOf(t *testing.T, raw string) string {
	t.Helper()
	address, err := url.Parse(raw)
	qt.New(t).Assert(err, qt.IsNil)
	return address.RequestURI()
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// Starting a sign-in sends the browser to the provider and binds the
// attempt to this browser with a cookie of its own: __Host-, Secure,
// HttpOnly, and Lax so it comes back from the provider's site.
func TestAnOIDCSignInStartsAtTheProvider(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	issuer := newIssuer(t)
	console := startOIDC(t, issuer)

	started := console.visit(t, "/auth/oidc/login")

	c.Assert(started.StatusCode, qt.Equals, http.StatusFound)
	c.Assert(strings.HasPrefix(started.Header.Get("Location"), issuer.URL()+"/authorize?"), qt.IsTrue)
	bound := cookieNamed(started.Cookies(), "__Host-ptah_console_oidc")
	c.Assert(bound, qt.IsNotNil)
	c.Assert(bound.Value, qt.Equals, mustQueryValue(t, started.Header.Get("Location"), "state"))
	c.Assert(bound.Secure, qt.IsTrue)
	c.Assert(bound.HttpOnly, qt.IsTrue)
	c.Assert(bound.SameSite, qt.Equals, http.SameSiteLaxMode)
	c.Assert(bound.Path, qt.Equals, "/")
	c.Assert(bound.MaxAge, qt.Equals, 600)
	c.Assert(started.Header.Get("Cache-Control"), qt.Equals, "no-store")
}

func mustQueryValue(t *testing.T, raw, key string) string {
	t.Helper()
	address, err := url.Parse(raw)
	qt.New(t).Assert(err, qt.IsNil)
	return address.Query().Get(key)
}

// A callback signs nobody in unless it is this browser's own attempt and
// the provider approved it; each refusal goes back to the sign-in page with
// a reason, and sets no session cookie.
func TestAnOIDCCallbackRefusesWhatIsNotThisBrowsersSignIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// callback is what the browser comes back with, from the start's
		// answer.
		callback func(t *testing.T, issuer *oidctest.Provider, started *http.Response) (string, []*http.Cookie)
		reason   string
	}{
		{name: "another browser's callback, without the cookie",
			callback: func(t *testing.T, issuer *oidctest.Provider, started *http.Response) (string, []*http.Cookie) {
				back, err := issuer.Approve(started.Header.Get("Location"), oidctest.Person{Subject: "alex"})
				qt.New(t).Assert(err, qt.IsNil)
				return pathOf(t, back), nil
			}, reason: "state"},
		{name: "a cookie for another attempt",
			callback: func(t *testing.T, issuer *oidctest.Provider, started *http.Response) (string, []*http.Cookie) {
				back, err := issuer.Approve(started.Header.Get("Location"), oidctest.Person{Subject: "alex"})
				qt.New(t).Assert(err, qt.IsNil)
				return pathOf(t, back), []*http.Cookie{{
					Name: "__Host-ptah_console_oidc", Value: "another-attempt",
					Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
				}}
			}, reason: "state"},
		{name: "a provider that refused",
			callback: func(_ *testing.T, _ *oidctest.Provider, started *http.Response) (string, []*http.Cookie) {
				state := started.Cookies()[0].Value
				return "/auth/oidc/callback?error=access_denied&state=" + url.QueryEscape(state), started.Cookies()
			}, reason: "provider"},
		{name: "a code the provider never gave",
			callback: func(_ *testing.T, _ *oidctest.Provider, started *http.Response) (string, []*http.Cookie) {
				state := started.Cookies()[0].Value
				return "/auth/oidc/callback?code=forged&state=" + url.QueryEscape(state), started.Cookies()
			}, reason: "token"},
		{name: "an API server that does not answer",
			callback: func(t *testing.T, issuer *oidctest.Provider, started *http.Response) (string, []*http.Cookie) {
				back, err := issuer.Approve(started.Header.Get("Location"), oidctest.Person{Subject: "alex"})
				qt.New(t).Assert(err, qt.IsNil)
				return pathOf(t, back), started.Cookies()
			}, reason: "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			issuer := newIssuer(t)
			console := startOIDC(t, issuer)
			started := console.visit(t, "/auth/oidc/login")
			path, cookies := test.callback(t, issuer, started)

			back := console.visit(t, path, cookies...)

			c.Assert(back.StatusCode, qt.Equals, http.StatusSeeOther)
			c.Assert(back.Header.Get("Location"), qt.Equals, "/login?error="+test.reason)
			c.Assert(cookieNamed(back.Cookies(), "__Host-ptah_console"), qt.IsNil)
			cleared := cookieNamed(back.Cookies(), "__Host-ptah_console_oidc")
			c.Assert(cleared, qt.IsNotNil)
			c.Assert(cleared.MaxAge, qt.Equals, -1)
		})
	}
}

// The console says how people sign in, and refuses a password.
func TestAnOIDCConsoleSaysHowToSignIn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	console := startOIDC(t, newIssuer(t))

	info := console.call(t, "InfoService/GetInfo", "{}")
	password := console.call(t, "SessionService/Login", login("alex@example.com", "alex-secret"))

	c.Assert(info.status, qt.Equals, http.StatusOK)
	c.Assert(info.body["signIn"], qt.Equals, "SIGN_IN_OIDC")
	c.Assert(info.body["signInProvider"], qt.Equals, "Example SSO")
	c.Assert(password.code, qt.Equals, "unimplemented")
	c.Assert(password.message, qt.Equals, "this console signs in with Example SSO, not a password")
	c.Assert(password.cookies, qt.HasLen, 0)
}

// A configuration with oidc is refused at start beside a passwords file, a
// client secret file that is empty, or a provider whose discovery cannot be
// read; one with accounts is refused beside a client secret file.
func TestAnOIDCConsoleRefusesToStartWhenMisconfigured(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	issuer := newIssuer(t)
	dir := t.TempDir()
	oidcPath := filepath.Join(dir, "oidc.yaml")
	c.Assert(os.WriteFile(oidcPath, []byte(oidcConfiguration(issuer)), 0o600), qt.IsNil)
	unreachable := filepath.Join(dir, "unreachable.yaml")
	c.Assert(os.WriteFile(unreachable, []byte(strings.Replace(oidcConfiguration(issuer), issuer.URL(), "https://127.0.0.1:1", 1)), 0o600), qt.IsNil)
	empty := filepath.Join(dir, "secret")
	c.Assert(os.WriteFile(empty, []byte("\n"), 0o600), qt.IsNil)
	accountsPath, passwordsPath := files(t)
	base := func(options hosted.Options) hosted.Options {
		options.Listen = "127.0.0.1:0"
		options.TLSTerminatedUpstream = true
		options.Kube = &rest.Config{Host: "https://127.0.0.1:1"}
		options.Assets = assets
		return options
	}

	c.Assert(hosted.Run(t.Context(), base(hosted.Options{ConfigPath: oidcPath, PasswordsPath: passwordsPath})),
		qt.ErrorMatches, `.*signs people in with oidc, which reads no passwords file; leave out --passwords`)
	c.Assert(hosted.Run(t.Context(), base(hosted.Options{ConfigPath: oidcPath, OIDCClientSecretFile: empty})),
		qt.ErrorMatches, `--oidc-client-secret-file is empty; leave the flag out for a public client`)
	c.Assert(hosted.Run(t.Context(), base(hosted.Options{ConfigPath: unreachable})),
		qt.ErrorMatches, `oidc: read the discovery document of https://127.0.0.1:1: .*`)
	c.Assert(hosted.Run(t.Context(), base(hosted.Options{ConfigPath: accountsPath, PasswordsPath: passwordsPath, OIDCClientSecretFile: empty})),
		qt.ErrorMatches, `--oidc-client-secret-file is for a configuration with oidc; this one lists accounts`)
	c.Assert(hosted.Run(t.Context(), base(hosted.Options{ConfigPath: accountsPath})),
		qt.ErrorMatches, `.*lists accounts, whose passwords are a file of their own; give --passwords`)
}
