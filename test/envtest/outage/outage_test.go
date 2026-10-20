package outage_test

import (
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{OperatorCRDs: true}, &plane))
}

// proxy forwards TCP to the API server, and can freeze: bytes it reads are
// held until it thaws, the way a paused load balancer holds them.
type proxy struct {
	listener net.Listener
	upstream string
	frozen   atomic.Bool
}

func newProxy(t *testing.T, upstream string) *proxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	qt.New(t).Assert(err, qt.IsNil)
	p := &proxy{listener: listener, upstream: upstream}
	go p.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return p
}

func (p *proxy) serve() {
	for {
		down, err := p.listener.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = down.Close()
			continue
		}
		go p.pipe(up, down)
		go p.pipe(down, up)
	}
}

func (p *proxy) pipe(to io.WriteCloser, from io.ReadCloser) {
	defer func() { _ = to.Close() }()
	defer func() { _ = from.Close() }()
	buffer := make([]byte, 32<<10)
	for {
		n, err := from.Read(buffer)
		for p.frozen.Load() {
			time.Sleep(20 * time.Millisecond)
		}
		if n > 0 {
			if _, werr := to.Write(buffer[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func through(t *testing.T, config *rest.Config, p *proxy) *rest.Config {
	t.Helper()
	server, err := url.Parse(config.Host)
	qt.New(t).Assert(err, qt.IsNil)
	copied := rest.CopyConfig(config)
	copied.Host = server.Scheme + "://" + p.listener.Addr().String()
	// The certificate names the API server's own address, not the proxy's.
	copied.TLSClientConfig.ServerName = server.Hostname()
	return copied
}

type factory struct{ client *kube.Client }

func (f factory) For(kube.Principal) (*kube.Client, error) { return f.client, nil }

// subscribe opens a stream as an identity that may watch the namespace,
// through the proxy.
func subscribe(t *testing.T, namespace string, p *proxy) events {
	t.Helper()
	c := qt.New(t)
	ctx := t.Context()
	user, err := plane.User("watcher-" + namespace)
	c.Assert(err, qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "watcher"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "watcher-" + namespace}},
	}), qt.IsNil)
	built, err := kube.NewClient(through(t, user, p))
	c.Assert(err, qt.IsNil)
	store := session.NewStore(session.Options{})
	token, _, err := store.Create(kube.Principal{Mode: kube.ModeLocal, Context: "envtest", Generation: 1})
	c.Assert(err, qt.IsNil)
	rpc := connect.NewServer(session.Interceptor(store, session.Cookies{}))
	consolev1connect.RegisterWatchServiceHandler(rpc, api.NewWatchService(factory{built}, api.Scope{},
		api.WatchOptions{Window: time.Second, Heartbeat: 100 * time.Millisecond}))
	streamCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancel)
	streamCtx, info := connect.NewClientContext(streamCtx)
	info.RequestHeader().Set("Cookie", "ptah_console="+token)
	subscription, err := consolev1connect.NewWatchServiceClient(connect.NewClient(connectinprocess.New(rpc))).
		Subscribe(streamCtx, &consolev1.SubscribeRequest{Namespaces: []string{namespace}})
	c.Assert(err, qt.IsNil)
	return read(subscription)
}

func apiServer(t *testing.T) string {
	t.Helper()
	server, err := url.Parse(plane.Env.Config.Host)
	qt.New(t).Assert(err, qt.IsNil)
	return server.Host
}

// While the API server does not answer, the stream says each watch is lost
// and being reopened, and sends no heartbeat that would call the page
// current. When it answers again, each kind is read again and a heartbeat
// follows.
func TestAnOutageIsReportedAndRecovered(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	p := newProxy(t, apiServer(t))
	stream := subscribe(t, "outage", p)
	_, live := stream.until(isHeartbeat, 20*time.Second)
	c.Assert(live, qt.IsTrue)

	p.frozen.Store(true)
	_, lost := stream.until(isReconnecting, 30*time.Second)
	// The page is behind now: no heartbeat may follow while it is.
	quiet := stream.during(3 * time.Second)
	p.frozen.Store(false)
	recovered, back := stream.until(isHeartbeat, 30*time.Second)

	c.Assert(lost, qt.IsTrue)
	c.Assert(count(quiet, isHeartbeat), qt.Equals, 0)
	c.Assert(back, qt.IsTrue)
	c.Assert(count(recovered, isWholeKind), qt.Not(qt.Equals), 0)
}

// A watch that keeps failing is reported each time it fails, not once and
// then left to wait: after the first loss each attempt starts with a fresh
// list, and that list is bounded.
func TestALastingOutageIsReportedAgain(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	p := newProxy(t, apiServer(t))
	stream := subscribe(t, "lasting", p)
	_, live := stream.until(isHeartbeat, 20*time.Second)
	c.Assert(live, qt.IsTrue)
	p.frozen.Store(true)
	seen := make(map[consolev1.WatchedKind]int)

	_, again := stream.until(lostTwice(seen), 30*time.Second)

	c.Assert(again, qt.IsTrue, qt.Commentf("losses per kind: %v", seen))
}
