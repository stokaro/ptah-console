package hosted_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/hosted"
)

const publicHost = "console.example.test"

var stopped = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

var assets = fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>console</title>")}}

const configuration = `apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: test
publicURL: https://console.example.test
namespaces: [storefront]
accounts:
  - id: alex
    login: alex@example.com
    displayName: Alex Morgan
  - id: sam
    login: sam@example.com
`

var passwords = sync.OnceValues(func() (string, error) {
	file := ""
	for _, account := range []string{"alex", "sam"} {
		hash, err := accounts.Hash([]byte(account+"-secret"), accounts.MinCost)
		if err != nil {
			return "", err
		}
		file += account + ":" + string(hash) + "\n"
	}
	return file, nil
})

// files writes the configuration and its passwords and returns their paths.
func files(t *testing.T) (configPath, passwordsPath string) {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	content, err := passwords()
	c.Assert(err, qt.IsNil)
	configPath, passwordsPath = filepath.Join(dir, "configuration.yaml"), filepath.Join(dir, "passwords")
	c.Assert(os.WriteFile(configPath, []byte(configuration), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(passwordsPath, []byte(content), 0o600), qt.IsNil)
	return configPath, passwordsPath
}

func TestItDoesNotStartWithoutHTTPS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		options hosted.Options
		says    string
	}{
		{name: "neither TLS nor a proxy that terminates it", options: hosted.Options{},
			says: "a hosted console serves HTTPS only: give --tls-cert-file and --tls-key-file, " +
				"or --tls-terminated-upstream when a proxy in front of it terminates TLS"},
		{name: "a certificate without its key", options: hosted.Options{TLSCertFile: "tls.crt"},
			says: "--tls-cert-file and --tls-key-file go together"},
		{name: "a key without its certificate", options: hosted.Options{TLSKeyFile: "tls.key"},
			says: "--tls-cert-file and --tls-key-file go together"},
		{name: "both", options: hosted.Options{TLSCertFile: "tls.crt", TLSKeyFile: "tls.key", TLSTerminatedUpstream: true},
			says: "give a certificate or --tls-terminated-upstream, not both"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			// The files are absent: the transport is refused before they are
			// read, and before anything listens.
			test.options.ConfigPath = filepath.Join(t.TempDir(), "absent.yaml")
			test.options.PasswordsPath = filepath.Join(t.TempDir(), "absent")
			test.options.Listen = "127.0.0.1:0"

			err := hosted.Run(t.Context(), test.options)

			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

// Outside a Pod there is no ServiceAccount to act through, and the console
// says so rather than look for some other identity.
func TestItDoesNotStartWithoutItsOwnIdentity(t *testing.T) {
	c := qt.New(t)
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	configPath, passwordsPath := files(t)

	err := hosted.Run(t.Context(), hosted.Options{
		ConfigPath: configPath, PasswordsPath: passwordsPath, Listen: "127.0.0.1:0",
		TLSTerminatedUpstream: true, Assets: assets,
	})

	c.Assert(err, qt.ErrorMatches, "read the console's own Kubernetes identity: .*")
}

// certificate writes a self-signed certificate for the public host, with a
// serial of its own, and returns it.
func certificate(t *testing.T, certFile, keyFile string, serial int64) *x509.Certificate {
	t.Helper()
	c := qt.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c.Assert(err, qt.IsNil)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: publicHost},
		DNSNames:     []string{publicHost},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	c.Assert(err, qt.IsNil)
	keyDER, err := x509.MarshalECPrivateKey(key)
	c.Assert(err, qt.IsNil)
	c.Assert(os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600), qt.IsNil)
	parsed, err := x509.ParseCertificate(der)
	c.Assert(err, qt.IsNil)
	return parsed
}

type console struct {
	address  string
	roots    *x509.CertPool
	certFile string
	keyFile  string
}

// start runs a hosted console with its own TLS. Its Kubernetes identity
// points nowhere: signing in never reaches the API server.
func start(t *testing.T) *console {
	t.Helper()
	c := qt.New(t)
	configPath, passwordsPath := files(t)
	dir := t.TempDir()
	served := &console{roots: x509.NewCertPool(), certFile: filepath.Join(dir, "tls.crt"), keyFile: filepath.Join(dir, "tls.key")}
	served.roots.AddCert(certificate(t, served.certFile, served.keyFile, 1))
	listening := make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- hosted.Run(ctx, hosted.Options{
			ConfigPath: configPath, PasswordsPath: passwordsPath, Listen: "127.0.0.1:0",
			TLSCertFile: served.certFile, TLSKeyFile: served.keyFile,
			Kube: &rest.Config{Host: "https://127.0.0.1:1"}, Assets: assets,
			Listening: func(addr net.Addr) { listening <- addr },
			// Sign-in limits refill with time. A clock that stands still
			// keeps a run on a slow machine from refilling one mid-test.
			Now: func() time.Time { return stopped },
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

// trust adds a certificate the client accepts.
func (s *console) trust(certificate *x509.Certificate) { s.roots.AddCert(certificate) }

type answer struct {
	status  int
	code    string
	message string
	cookies []*http.Cookie
	serial  *big.Int
}

// call makes one Connect call over a connection of its own, so each call
// shows the certificate the console serves at that moment.
func (s *console) call(t *testing.T, procedure, body string, cookies ...*http.Cookie) answer {
	t.Helper()
	c := qt.New(t)
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: s.roots, ServerName: publicHost, MinVersion: tls.VersionTLS12},
		DisableKeepAlives: true,
	}}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://"+s.address+"/api/ptah.console.v1."+procedure, strings.NewReader(body))
	c.Assert(err, qt.IsNil)
	request.Host = publicHost
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := client.Do(request)
	c.Assert(err, qt.IsNil)
	defer response.Body.Close()
	var failure struct{ Code, Message string }
	_ = json.NewDecoder(response.Body).Decode(&failure)
	return answer{
		status: response.StatusCode, code: failure.Code, message: failure.Message,
		cookies: response.Cookies(), serial: response.TLS.PeerCertificates[0].SerialNumber,
	}
}

func login(name, password string) string {
	body, _ := json.Marshal(map[string]string{"login": name, "password": password})
	return string(body)
}

// The cookie a sign-in sets is the hosted one: __Host-, Secure, HttpOnly,
// SameSite=Strict, for the whole host and no other.
func TestSigningInSetsAHostOnlySecureCookie(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	console := start(t)

	signedIn := console.call(t, "SessionService/Login", login("Alex@Example.com", "alex-secret"))

	c.Assert(signedIn.status, qt.Equals, http.StatusOK, qt.Commentf("%s", signedIn.message))
	c.Assert(signedIn.cookies, qt.HasLen, 1)
	cookie := signedIn.cookies[0]
	c.Assert(cookie.Name, qt.Equals, "__Host-ptah_console")
	c.Assert(cookie.Value, qt.Not(qt.Equals), "")
	c.Assert(cookie.Secure, qt.IsTrue)
	c.Assert(cookie.HttpOnly, qt.IsTrue)
	c.Assert(cookie.SameSite, qt.Equals, http.SameSiteStrictMode)
	c.Assert(cookie.Path, qt.Equals, "/")
	c.Assert(cookie.Domain, qt.Equals, "")
}

// An unknown login and a wrong password get the same answer, and neither
// gets a cookie.
func TestARefusedSignInSaysTheSameWhateverWasWrong(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, login, password string
	}{
		{name: "a wrong password", login: "alex@example.com", password: "sam-secret"},
		{name: "an unknown login", login: "kim@example.com", password: "alex-secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			console := start(t)

			refused := console.call(t, "SessionService/Login", login(test.login, test.password))

			c.Assert(refused.status, qt.Equals, http.StatusUnauthorized)
			c.Assert(refused.code, qt.Equals, "unauthenticated")
			c.Assert(refused.message, qt.Equals, "the login or the password is wrong")
			c.Assert(refused.cookies, qt.HasLen, 0)
		})
	}
}

// Signing in again ends the session the browser had, so a cookie somebody
// learned before stops working; signing out ends the new one.
func TestSigningInAgainEndsTheEarlierSession(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	console := start(t)
	first := console.call(t, "SessionService/Login", login("alex@example.com", "alex-secret")).cookies[0]

	second := console.call(t, "SessionService/Login", login("alex@example.com", "alex-secret"), first).cookies[0]
	earlier := console.call(t, "SessionService/Logout", "{}", first)
	signedOut := console.call(t, "SessionService/Logout", "{}", second)
	after := console.call(t, "SessionService/Logout", "{}", second)

	c.Assert(second.Value, qt.Not(qt.Equals), first.Value)
	c.Assert(earlier.code, qt.Equals, "unauthenticated")
	c.Assert(signedOut.status, qt.Equals, http.StatusOK)
	c.Assert(signedOut.cookies, qt.HasLen, 1)
	c.Assert(signedOut.cookies[0].MaxAge, qt.Equals, -1)
	c.Assert(after.code, qt.Equals, "unauthenticated")
}

// After five wrong passwords a login waits, and the right one is turned away
// with the same answer as any throttled attempt.
func TestALoginThatTriedTooOftenIsToldToWait(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	console := start(t)
	for range 5 {
		c.Assert(console.call(t, "SessionService/Login", login("sam@example.com", "guess")).code, qt.Equals, "unauthenticated")
	}

	throttled := console.call(t, "SessionService/Login", login("sam@example.com", "sam-secret"))
	other := console.call(t, "SessionService/Login", login("alex@example.com", "alex-secret"))

	c.Assert(throttled.code, qt.Equals, "resource_exhausted")
	c.Assert(throttled.message, qt.Equals, "too many sign-in attempts; wait a minute and try again")
	c.Assert(throttled.cookies, qt.HasLen, 0)
	c.Assert(other.status, qt.Equals, http.StatusOK)
}

// A renewed certificate reaches the next connection without a restart.
func TestARenewedCertificateIsServedWithoutARestart(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	console := start(t)
	before := console.call(t, "InfoService/GetInfo", "{}")

	console.trust(certificate(t, console.certFile, console.keyFile, 2))
	later := time.Now().Add(time.Minute)
	c.Assert(os.Chtimes(console.certFile, later, later), qt.IsNil)
	c.Assert(os.Chtimes(console.keyFile, later, later), qt.IsNil)
	after := console.call(t, "InfoService/GetInfo", "{}")

	c.Assert(before.serial.Int64(), qt.Equals, int64(1))
	c.Assert(after.status, qt.Equals, http.StatusOK)
	c.Assert(after.serial.Int64(), qt.Equals, int64(2))
}
