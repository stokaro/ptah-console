package hosted_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/agentlink"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/hosted"
)

const (
	agentsThis     = "4a1b2c3d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	agentsExternal = "5b2c3d4e-6f70-4b8c-9dae-1f2a3b4c5d6e"
	agentsName     = "agents.envtest"
)

// pki is a CA and the agent listener's certificate from it, as files.
type pki struct {
	roots             *x509.CertPool
	caPEM             string
	certFile, keyFile string
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	c := qt.New(t)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c.Assert(err, qt.IsNil)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "console CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	c.Assert(err, qt.IsNil)
	ca, err := x509.ParseCertificate(caDER)
	c.Assert(err, qt.IsNil)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c.Assert(err, qt.IsNil)
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: agentsName}, DNSNames: []string{agentsName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	c.Assert(err, qt.IsNil)
	keyDER, err := x509.MarshalECPrivateKey(key)
	c.Assert(err, qt.IsNil)
	dir := t.TempDir()
	out := &pki{
		roots: x509.NewCertPool(), caPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		certFile: filepath.Join(dir, "tls.crt"), keyFile: filepath.Join(dir, "tls.key"),
	}
	out.roots.AddCert(ca)
	c.Assert(os.WriteFile(out.certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(out.keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600), qt.IsNil)
	return out
}

// arrangeRegistry is the registry namespace with this cluster's entry and an
// external one, and the grant the console chart gives the console in it.
func arrangeRegistry(t *testing.T) client.ObjectKey {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	group := consolev1alpha1.GroupVersion.Group
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "agents-"}}
	c.Assert(plane.Admin.Create(ctx, namespace), qt.IsNil)
	agentsNS := namespace.Name
	for _, object := range []client.Object{
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Namespace: agentsNS, Name: "housekeeping"},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{group}, Resources: []string{"consoleclusters", "consoleclusterpreferences"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{group}, Resources: []string{"consoleclusters/status"}, Verbs: []string{"update", "patch"}},
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Namespace: agentsNS, Name: "housekeeping"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "housekeeping"},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
		},
		&consolev1alpha1.ConsoleCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: agentsNS, Name: "this-cluster"},
			Spec: consolev1alpha1.ConsoleClusterSpec{
				ConnectionUID: agentsThis, Profile: consolev1alpha1.ProfileCoLocated, InstallationID: "envtest",
				Namespaces: []string{"storefront"}, ConfigurationOwner: consolev1alpha1.OwnerGitOps,
			},
		},
		&consolev1alpha1.ConsoleCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: agentsNS, Name: "prod-ap-2"},
			Spec: consolev1alpha1.ConsoleClusterSpec{
				ConnectionUID: agentsExternal, Profile: consolev1alpha1.ProfileAgent, InstallationID: "envtest",
				Namespaces: []string{"storefront"}, ConfigurationOwner: consolev1alpha1.OwnerConsole,
				Agent: &consolev1alpha1.AgentTrust{
					TargetServer: "https://api.prod-ap-2.example.com:6443", TargetCASHA256: "sha256:" + strings.Repeat("1", 64),
					ConsoleServerName: agentsName, ConsoleCASHA256: "sha256:" + strings.Repeat("2", 64),
				},
			},
		},
	} {
		c.Assert(plane.Admin.Create(ctx, object), qt.IsNil)
	}
	return client.ObjectKey{Namespace: agentsNS, Name: "prod-ap-2"}
}

// An agent dials the console's own agent listener with the credential the
// console issued, and the console records its channel with its own identity.
// Each listener serves its own services and not the other's, so a channel
// credential reaches no browser endpoint and a browser reaches no agent
// endpoint.
func TestAnAgentDialsTheConsolesAgentListener(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	key := arrangeRegistry(t)
	agentsNS := key.Namespace
	credential, err := agentlink.Issue(t.Context(), plane.Admin, key, time.Now(), agentlink.DefaultValidity)
	c.Assert(err, qt.IsNil)
	certificates := newPKI(t)
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
registry:
  namespace: `+agentsNS+`
  thisCluster: this-cluster
  connectionUID: `+agentsThis+`
`+registryAgents(certificates)), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(passwordsPath, []byte("alex:"+string(hash)+"\n"), 0o600), qt.IsNil)
	browser, agents := make(chan net.Addr, 1), make(chan net.Addr, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- hosted.Run(ctx, hosted.Options{
			ConfigPath: configPath, PasswordsPath: passwordsPath, Listen: "127.0.0.1:0",
			TLSTerminatedUpstream: true, Kube: consoleConfig(t),
			Assets:      fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
			Listening:   func(addr net.Addr) { browser <- addr },
			AgentListen: "127.0.0.1:0", AgentTLSCertFile: certificates.certFile, AgentTLSKeyFile: certificates.keyFile,
			AgentListening: func(addr net.Addr) { agents <- addr },
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	browserAddress, agentAddress := (<-browser).String(), (<-agents).String()

	opened := openAgent(t, agentAddress, certificates.roots, credential)
	deadline := time.Now().Add(10 * time.Second)
	entry := &consolev1alpha1.ConsoleCluster{}
	for !meta.IsStatusConditionTrue(entry.Status.Conditions, consolev1alpha1.ConditionConnected) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		c.Assert(plane.Admin.Get(t.Context(), key, entry), qt.IsNil)
	}

	c.Assert(meta.IsStatusConditionTrue(entry.Status.Conditions, consolev1alpha1.ConditionConnected), qt.IsTrue,
		qt.Commentf("the console never recorded the agent's channel"))
	c.Assert(entry.Status.Channel.Credentials[0].FirstUsedAt, qt.IsNotNil)
	status, contentType := responseOf(t, http.DefaultClient, "http://"+browserAddress+"/ptah.console.agent.v1.AgentService/Channel", "")
	c.Assert(contentType, qt.Matches, `text/html.*`, qt.Commentf("status %d: the browser listener answered as something other than its web app", status))
	agentClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: certificates.roots, ServerName: agentsName, MinVersion: tls.VersionTLS13}, ForceAttemptHTTP2: true,
	}}
	status, _ = responseOf(t, agentClient, "https://"+agentAddress+"/ptah.console.v1.SchemaService/ListSchemas", credential)
	c.Assert(status, qt.Equals, http.StatusNotFound)
	status, _ = responseOf(t, agentClient, "https://"+agentAddress+"/ptah.console.v1.SessionService/Login", credential)
	c.Assert(status, qt.Equals, http.StatusNotFound)
	opened()
}

// responseOf is the status and content type a POST to address gets, with
// the credential as a bearer token when one is given.
func responseOf(t *testing.T, httpClient *http.Client, address, credential string) (status int, contentType string) {
	t.Helper()
	c := qt.New(t)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, address, strings.NewReader("{}"))
	c.Assert(err, qt.IsNil)
	request.Host = "console.envtest"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	if credential != "" {
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := httpClient.Do(request)
	c.Assert(err, qt.IsNil)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response.StatusCode, response.Header.Get("Content-Type")
}

// openAgent holds an agent's channel to the address open, and returns what
// stops it.
func openAgent(t *testing.T, address string, roots *x509.CertPool, credential string) func() {
	t.Helper()
	c := qt.New(t)
	config, err := agent.ParseConfiguration([]byte(`apiVersion: console.ptah.run/v1alpha1
kind: AgentConfiguration
installationID: envtest
connection: {uid: ` + agentsExternal + `, generation: 1}
console: {address: https://` + address + `, serverName: ` + agentsName + `}
namespaces: [storefront]
`))
	c.Assert(err, qt.IsNil)
	factory, err := agent.NewFactory(plane.Env.Config, config)
	c.Assert(err, qt.IsNil)
	list, err := agent.ParseAccounts([]byte("apiVersion: console.ptah.run/v1alpha1\nkind: AgentAccounts\naccounts: [alex]\n"))
	c.Assert(err, qt.IsNil)
	c.Assert(factory.Set(list), qt.IsNil)
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_ = agent.Open(ctx, agent.ChannelOptions{
			Config: config, RootCAs: roots, Credential: func() (string, error) { return credential, nil },
		}, agent.NewDispatcher(factory, audit.New(io.Discard), nil))
	}()
	return func() {
		cancel()
		<-ended
	}
}

// registryAgents is the configuration's registry.agents block for a console
// whose agent listener has the certificates.
func registryAgents(certificates *pki) string {
	return "  agents:\n    address: https://" + agentsName + "\n    serverName: " + agentsName +
		"\n    caBundle: |\n      " + strings.ReplaceAll(strings.TrimSpace(certificates.caPEM), "\n", "\n      ") + "\n"
}
