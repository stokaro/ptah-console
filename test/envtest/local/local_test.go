package local_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	qt "github.com/frankban/quicktest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/stokaro/ptah-console/internal/local"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{}, &plane))
}

// console runs ptah-console local for a certificate-authenticated user and
// returns the launch link it would have opened.
func console(t *testing.T, user string, readOnly bool) (origin, link string) {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["envtest"] = &clientcmdapi.Cluster{Server: config.Host, CertificateAuthorityData: config.CAData}
	kubeconfig.AuthInfos[user] = &clientcmdapi.AuthInfo{ClientCertificateData: config.CertData, ClientKeyData: config.KeyData}
	kubeconfig.Contexts["envtest"] = &clientcmdapi.Context{Cluster: "envtest", AuthInfo: user}
	kubeconfig.CurrentContext = "envtest"
	path := filepath.Join(t.TempDir(), "kubeconfig")
	c.Assert(clientcmd.WriteToFile(*kubeconfig, path), qt.IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	links := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- local.Run(ctx, local.Options{
			Kubeconfig: path,
			ReadOnly:   readOnly,
			StatePath:  filepath.Join(t.TempDir(), "state.json"),
			Assets:     fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
			Stdout:     io.Discard,
			Open:       func(url string) error { links <- url; return nil },
		})
	}()
	t.Cleanup(func() {
		cancel()
		c.Assert(<-done, qt.IsNil)
	})
	select {
	case link = <-links:
	case err := <-done:
		t.Fatalf("ptah-console local stopped: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("ptah-console local opened no link")
	}
	return link[:strings.Index(link, "/#t=")], link
}

// rpc sends one Connect unary call the way the browser does.
func rpc(t *testing.T, origin, procedure, body string, cookies ...*http.Cookie) (*http.Response, map[string]any) {
	t.Helper()
	c := qt.New(t)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin+"/api/ptah.console.v1."+procedure, strings.NewReader(body))
	c.Assert(err, qt.IsNil)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("Origin", origin)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := http.DefaultClient.Do(request)
	c.Assert(err, qt.IsNil)
	defer response.Body.Close()
	decoded := make(map[string]any)
	data, err := io.ReadAll(response.Body)
	c.Assert(err, qt.IsNil)
	_ = json.Unmarshal(data, &decoded)
	return response, decoded
}

func TestTheLaunchLinkSignsInOnceAsTheKubeconfigsUser(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	origin, link := console(t, "local-person", false)
	token := link[strings.Index(link, "#t=")+3:]

	exchanged, _ := rpc(t, origin, "SessionService/ExchangeLaunchToken", `{"token":"`+token+`"}`)
	cookies := exchanged.Cookies()
	c.Assert(exchanged.StatusCode, qt.Equals, http.StatusOK)
	c.Assert(cookies, qt.HasLen, 1)
	c.Assert(cookies[0].Name, qt.Equals, "ptah_console")
	c.Assert(cookies[0].HttpOnly, qt.IsTrue)
	c.Assert(cookies[0].SameSite, qt.Equals, http.SameSiteStrictMode)

	who, session := rpc(t, origin, "SessionService/GetSession", `{}`, cookies[0])
	c.Assert(who.StatusCode, qt.Equals, http.StatusOK)
	c.Assert(session["kubernetesUsername"], qt.Equals, "local-person")
	c.Assert(session["context"], qt.Equals, "envtest")
	c.Assert(session["mode"], qt.Equals, "MODE_LOCAL")

	again, _ := rpc(t, origin, "SessionService/ExchangeLaunchToken", `{"token":"`+token+`"}`)
	c.Assert(again.StatusCode, qt.Equals, http.StatusForbidden)
	c.Assert(again.Cookies(), qt.HasLen, 0)

	anonymous, _ := rpc(t, origin, "SessionService/GetSession", `{}`)
	c.Assert(anonymous.StatusCode, qt.Equals, http.StatusUnauthorized)

	out, _ := rpc(t, origin, "SessionService/Logout", `{}`, cookies[0])
	c.Assert(out.StatusCode, qt.Equals, http.StatusOK)
	after, _ := rpc(t, origin, "SessionService/GetSession", `{}`, cookies[0])
	c.Assert(after.StatusCode, qt.Equals, http.StatusUnauthorized)
}

// Another page in the same browser does not have the link, and a page that
// rebinds its name to the loopback address is refused before it asks.
func TestOnlyTheLinkSignsIn(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	origin, _ := console(t, "careful-person", false)

	guessed, _ := rpc(t, origin, "SessionService/ExchangeLaunchToken", `{"token":"guess"}`)
	c.Assert(guessed.StatusCode, qt.Equals, http.StatusForbidden)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin+"/", nil)
	c.Assert(err, qt.IsNil)
	request.Host = "attacker.example"
	rebound, err := http.DefaultClient.Do(request)
	c.Assert(err, qt.IsNil)
	_ = rebound.Body.Close()
	c.Assert(rebound.StatusCode, qt.Equals, http.StatusMisdirectedRequest)
}

// --read-only refuses every decision on the server, whatever the page sends,
// and still answers reads.
func TestReadOnlyRefusesDecisions(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	origin, link := console(t, "read-only-person", true)
	exchanged, _ := rpc(t, origin, "SessionService/ExchangeLaunchToken", `{"token":"`+link[strings.Index(link, "#t=")+3:]+`"}`)
	c.Assert(exchanged.StatusCode, qt.Equals, http.StatusOK)
	cookie := exchanged.Cookies()[0]

	approve, approveBody := rpc(t, origin, "DecisionService/ApproveSchemaPlan",
		`{"namespace":"default","schemaName":"storefront","schemaUid":"u","planName":"p","planUid":"u","planFingerprint":"f"}`, cookie)
	suspend, suspendBody := rpc(t, origin, "DecisionService/SetSuspended",
		`{"kind":"RESOURCE_KIND_SCHEMA","namespace":"default","name":"storefront","uid":"u","resourceVersion":"1","suspended":true}`, cookie)
	list, _ := rpc(t, origin, "DecisionService/ListDecisions", `{"namespaces":["default"]}`, cookie)

	c.Assert(approve.StatusCode, qt.Equals, http.StatusForbidden)
	c.Assert(approveBody["code"], qt.Equals, "permission_denied")
	c.Assert(suspend.StatusCode, qt.Equals, http.StatusForbidden)
	c.Assert(suspendBody["code"], qt.Equals, "permission_denied")
	c.Assert(list.StatusCode, qt.Equals, http.StatusOK)
}
