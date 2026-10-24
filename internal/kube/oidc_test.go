package kube_test

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/kube"
)

func oidcPrincipal(signIn string) kube.Principal {
	return kube.Principal{Mode: kube.ModeOIDC, AccountID: signIn, Context: kube.HostedConnection, Generation: 3}
}

// staticBearer sets one token on every request.
type staticBearer string

func (b staticBearer) Transport(next http.RoundTripper) http.RoundTripper {
	return roundTrip(func(request *http.Request) (*http.Response, error) {
		signed := request.Clone(request.Context())
		signed.Header.Set("Authorization", "Bearer "+string(b))
		return next.RoundTrip(signed)
	})
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

// seen is what an API server was sent: each request's credentials.
type seen struct {
	mu       sync.Mutex
	requests []string
}

func (s *seen) add(request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	certificates := len(request.TLS.PeerCertificates)
	s.requests = append(s.requests, request.Header.Get("Authorization")+"|"+
		request.Header.Get("Impersonate-User")+"|"+strconv.Itoa(certificates))
}

func (s *seen) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// A sign-in's client sends the person's token and nothing else that
// authenticates, whatever the console's own configuration carries: its
// ServiceAccount token and token file, a client certificate, an exec plugin
// and an impersonation are all left behind.
func TestAnOIDCClientSendsThePersonsTokenAlone(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	api := &seen{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.add(r)
		http.NotFound(w, r)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	token := filepath.Join(t.TempDir(), "token")
	c.Assert(os.WriteFile(token, []byte("console-service-account"), 0o600), qt.IsNil)
	clientCertificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	cluster := &rest.Config{
		Host:            server.URL,
		BearerToken:     "console-service-account",
		BearerTokenFile: token,
		Username:        "admin", Password: "secret",
		TLSClientConfig: rest.TLSClientConfig{CAData: ca, CertData: clientCertificate},
		ExecProvider:    &clientcmdapi.ExecConfig{Command: "/bin/false", APIVersion: "client.authentication.k8s.io/v1"},
		Impersonate:     rest.ImpersonationConfig{UserName: "system:admin"},
	}
	factory, err := kube.NewOIDCFactory(cluster, 3)
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Add("sign-in-1", staticBearer("alex-id-token")), qt.IsNil)
	person, err := factory.For(oidcPrincipal("sign-in-1"))
	c.Assert(err, qt.IsNil)

	_ = person.Get(t.Context(), client.ObjectKey{Name: "storefront"}, &corev1.Namespace{})

	requests := api.all()
	c.Assert(len(requests) > 0, qt.IsTrue)
	for _, request := range requests {
		c.Assert(request, qt.Equals, "Bearer alex-id-token||0")
	}
	c.Assert(person.Config.BearerToken, qt.Equals, "")
	c.Assert(person.Config.BearerTokenFile, qt.Equals, "")
	c.Assert(person.Config.ExecProvider, qt.IsNil)
	c.Assert(cluster.BearerToken, qt.Equals, "console-service-account", qt.Commentf("the console's own configuration is left as it was"))
}

// A configuration that does not verify the API server is refused: the
// person's token would go to whoever answers.
func TestAnOIDCFactoryRefusesAnUnverifiedServer(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	_, err := kube.NewOIDCFactory(&rest.Config{Host: "https://10.96.0.1", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, 3)

	c.Assert(err, qt.ErrorMatches, `kube: the API server's certificate is not verified; .*`)
}

func TestTheOIDCFactoryRefusesAnyoneElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		principal kube.Principal
		says      string
	}{
		{name: "a sign-in it does not hold", principal: oidcPrincipal("sign-in-2"),
			says: `kube: sign-in "sign-in-2" has ended`},
		{name: "a hosted account", principal: hostedPrincipal("sign-in-1"),
			says: `kube: an OIDC console cannot act for a hosted principal`},
		{name: "another connection", principal: kube.Principal{Mode: kube.ModeOIDC, AccountID: "sign-in-1", Context: "elsewhere", Generation: 3},
			says: `kube: an OIDC console acts through "in-cluster", not "elsewhere"`},
		{name: "another generation", principal: kube.Principal{Mode: kube.ModeOIDC, AccountID: "sign-in-1", Context: kube.HostedConnection, Generation: 2},
			says: `kube: the configuration is generation 3, not 2`},
		{name: "no sign-in", principal: kube.Principal{Mode: kube.ModeOIDC, Context: kube.HostedConnection, Generation: 3},
			says: `kube: an OIDC principal needs its sign-in`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			factory, err := kube.NewOIDCFactory(&rest.Config{Host: "https://10.96.0.1"}, 3)
			c.Assert(err, qt.IsNil)
			c.Assert(factory.Add("sign-in-1", staticBearer("token")), qt.IsNil)

			client, err := factory.For(test.principal)

			c.Assert(client, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.says)
		})
	}
}

// A sign-in that ended has no client any more, and an ID is held once.
func TestASignInsClientEndsWithIt(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	factory, err := kube.NewOIDCFactory(&rest.Config{Host: "https://10.96.0.1"}, 3)
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Add("sign-in-1", staticBearer("token")), qt.IsNil)
	c.Assert(factory.Add("sign-in-1", staticBearer("other")), qt.ErrorMatches, `kube: sign-in "sign-in-1" already has a client`)

	factory.Remove("sign-in-1")

	_, err = factory.For(oidcPrincipal("sign-in-1"))
	c.Assert(err, qt.ErrorMatches, `kube: sign-in "sign-in-1" has ended`)
}
