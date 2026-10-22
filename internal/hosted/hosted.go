// Package hosted runs the console beside the operator, for people who sign in
// with a Console account and act as the account's Kubernetes username.
//
// It serves HTTPS or nothing: its own TLS from certificate files it reads
// again when they change, or plain HTTP behind a proxy the installer declares
// terminates TLS. Without one of the two it does not start.
package hosted

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"connectrpc.com/connect/v2"
	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/auth/clientaddr"
	"github.com/stokaro/ptah-console/internal/auth/session"
	"github.com/stokaro/ptah-console/internal/auth/signin"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/registry"
	"github.com/stokaro/ptah-console/internal/route"
	"github.com/stokaro/ptah-console/internal/server"
)

// generation is the configuration's. A new configuration is a new process,
// whose sessions all start over.
const generation = 1

// Options configure a hosted console.
type Options struct {
	ConfigPath    string
	PasswordsPath string
	// Listen is the address to serve on, such as ":8443".
	Listen string
	// TLSCertFile and TLSKeyFile serve HTTPS with a certificate read again
	// whenever either file changes.
	TLSCertFile string
	TLSKeyFile  string
	// TLSTerminatedUpstream declares that a proxy in front of the console
	// terminates HTTPS, so the console serves plain HTTP to it.
	TLSTerminatedUpstream bool
	ReadOnly              bool
	// Kube is the console's own configuration. Nil reads the one Kubernetes
	// gives a Pod.
	Kube   *rest.Config
	Assets fs.FS
	Logger *slog.Logger
	Audit  *audit.Logger
	Stdout io.Writer
	// Listening, when set, is told the address the console listens on.
	Listening func(net.Addr)
	// AgentListen, when set, serves the agents of the registry's connections
	// on an address of their own, with a certificate of its own from the
	// console's CA: AgentTLSCertFile and AgentTLSKeyFile, read again when
	// either changes.
	AgentListen      string
	AgentTLSCertFile string
	AgentTLSKeyFile  string
	// AgentListening, when set, is told the address agents dial.
	AgentListening func(net.Addr)
	// Now is the clock sign-in limits are kept by; nil is time.Now.
	Now func() time.Time
	// PerLogin and PerAddress override the sign-in limits; zero keeps
	// signin's defaults.
	PerLogin, PerAddress signin.Limits
}

// transportProblem says why the options do not serve HTTPS.
func (o Options) transportProblem() error {
	files := o.TLSCertFile != "" || o.TLSKeyFile != ""
	switch {
	case files && o.TLSTerminatedUpstream:
		return errors.New("give a certificate or --tls-terminated-upstream, not both")
	case files && (o.TLSCertFile == "" || o.TLSKeyFile == ""):
		return errors.New("--tls-cert-file and --tls-key-file go together")
	case !files && !o.TLSTerminatedUpstream:
		return errors.New("a hosted console serves HTTPS only: give --tls-cert-file and --tls-key-file, " +
			"or --tls-terminated-upstream when a proxy in front of it terminates TLS")
	}
	return nil
}

// Run serves the console until ctx ends.
func Run(ctx context.Context, options Options) error {
	if err := options.transportProblem(); err != nil {
		return err
	}
	options.defaults()
	config, passwords, err := accounts.Load(options.ConfigPath, options.PasswordsPath)
	if err != nil {
		return err
	}
	if err := options.agentsProblem(config); err != nil {
		return err
	}
	kubeConfig := options.Kube
	if kubeConfig == nil {
		if kubeConfig, err = rest.InClusterConfig(); err != nil {
			return fmt.Errorf("read the console's own Kubernetes identity: %w", err)
		}
	}
	if err := checkThisCluster(ctx, kubeConfig, config); err != nil {
		return err
	}
	agentListener, err := options.listenForAgents(kubeConfig, config)
	if err != nil {
		return err
	}
	browser, err := options.browser(config, passwords, kubeConfig, agentListener)
	if err != nil {
		if agentListener != nil {
			_ = agentListener.listener.Close()
		}
		return err
	}
	listener, err := net.Listen("tcp", options.Listen)
	if err != nil {
		if agentListener != nil {
			_ = agentListener.listener.Close()
		}
		return fmt.Errorf("listen on %s: %w", options.Listen, err)
	}
	if options.Listening != nil {
		options.Listening(listener.Addr())
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go browser.store.Run(ctx, time.Minute)
	served := server.Serve(ctx, server.NewHTTPServer(browser.handler, browser.tls, options.Logger), []net.Listener{listener})
	agentsServed := make(<-chan error)
	if agentListener != nil {
		if agentsServed, err = agentListener.serve(ctx, kubeConfig, config.Registry.Namespace, options.Now, options.Logger); err != nil {
			return err
		}
		if options.AgentListening != nil {
			options.AgentListening(agentListener.listener.Addr())
		}
		fmt.Fprintf(options.Stdout, "Agents of external clusters dial %s\n", agentListener.listener.Addr())
	}
	transport := "HTTPS"
	if browser.tls == nil {
		transport = "HTTP behind a proxy that terminates TLS"
	}
	fmt.Fprintf(options.Stdout, "Ptah Console for installation %s serves %s on %s, over %s\n",
		config.InstallationID, browser.origin, listener.Addr(), transport)
	select {
	case err := <-served:
		return err
	case err := <-agentsServed:
		cancel()
		<-served
		return err
	}
}

// defaults fills what the caller left out.
func (o *Options) defaults() {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Audit == nil {
		o.Audit = audit.Discard()
	}
	if o.Stdout == nil {
		o.Stdout = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// browserSide is what serves people: the web app and the console's services
// behind a session, on the browser's listener.
type browserSide struct {
	handler http.Handler
	store   *session.Store
	tls     *tls.Config
	origin  string
}

func (o Options) browser(config *accounts.Config, passwords *accounts.Passwords, kubeConfig *rest.Config, agents *agents) (*browserSide, error) {
	usernames := make(map[string]string, len(config.Accounts))
	displayNames := make(map[string]string, len(config.Accounts))
	for _, account := range config.Accounts {
		usernames[account.ID] = config.Username(account)
		displayNames[account.ID] = account.DisplayName
	}
	factory, err := kube.NewHostedFactory(kubeConfig, usernames, generation)
	if err != nil {
		return nil, err
	}
	verifier, err := signin.New(signin.Options{
		Config: config, Passwords: passwords, Now: o.Now,
		PerLogin: o.PerLogin, PerAddress: o.PerAddress,
	})
	if err != nil {
		return nil, err
	}
	tlsConfig, err := o.tlsConfig()
	if err != nil {
		return nil, err
	}
	origin, err := originOf(config.PublicURL)
	if err != nil {
		return nil, err
	}
	store := session.NewStore(session.Options{})
	cookies := session.Cookies{Secure: true}
	sessions := api.NewHostedSession(store, cookies, api.HostedAccounts{
		Verifier: verifier, Generation: generation, DisplayNames: displayNames,
		Addresses: clientaddr.New(config.Proxies()),
	}, factory, api.SessionFacts{Mode: consolev1.Mode_MODE_HOSTED, ReadOnly: o.ReadOnly}, o.Audit)
	scope := api.Scope{Candidates: config.Namespaces, Ceiling: config.Namespaces}
	interceptors := []connect.ServerInterceptor{
		session.Interceptor(store, cookies,
			consolev1connect.InfoServiceGetInfoProcedure,
			consolev1connect.SessionServiceLoginProcedure),
	}
	if o.ReadOnly {
		interceptors = append(interceptors, api.ReadOnly(
			consolev1connect.SessionServiceLoginProcedure,
			consolev1connect.SessionServiceLogoutProcedure))
	}
	if agents != nil {
		// After the session and read-only checks: a routed request is a
		// signed-in person's, and a read-only console sends no change to any
		// cluster.
		housekeeping, err := kube.NewClient(kubeConfig)
		if err != nil {
			return nil, err
		}
		router := route.New(agents.hub, housekeeping, factory, config.Registry.Namespace, config.Registry.ConnectionUID)
		interceptors = append(interceptors, router.Interceptor())
	}
	handler, err := server.New(server.Options{
		Mode:         consolev1.Mode_MODE_HOSTED,
		ReadOnly:     o.ReadOnly,
		Assets:       o.Assets,
		Origins:      []string{origin},
		Logger:       o.Logger,
		Interceptors: interceptors,
		Services: []func(*connect.Server){func(rpc *connect.Server) {
			api.Register(rpc, sessions, factory, scope, o.Audit)
		}},
	})
	if err != nil {
		return nil, err
	}
	return &browserSide{handler: handler, store: store, tls: tlsConfig, origin: origin}, nil
}

// tlsConfig is nil behind a proxy that terminates TLS.
func (o Options) tlsConfig() (*tls.Config, error) {
	if o.TLSTerminatedUpstream {
		return nil, nil
	}
	certificate := &certificateFiles{certFile: o.TLSCertFile, keyFile: o.TLSKeyFile, logger: o.Logger}
	if _, err := certificate.get(nil); err != nil {
		return nil, fmt.Errorf("load the TLS certificate: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: certificate.get}, nil
}

// originOf is the scheme and host of the public URL, which is what the
// server compares a request's Host and Origin with.
func originOf(publicURL string) (string, error) {
	address, err := url.Parse(publicURL)
	if err != nil {
		return "", fmt.Errorf("publicURL: %w", err)
	}
	return address.Scheme + "://" + address.Host, nil
}

// checkThisCluster refuses to start when the registry is on and this
// cluster's entry disagrees with the configuration the console was given.
func checkThisCluster(ctx context.Context, kubeConfig *rest.Config, config *accounts.Config) error {
	if config.Registry == nil {
		return nil
	}
	housekeeping, err := kube.NewClient(kubeConfig)
	if err != nil {
		return err
	}
	return registry.CheckThisCluster(ctx, housekeeping, registry.ThisCluster{
		Namespace: config.Registry.Namespace, Name: config.Registry.ThisCluster,
		ConnectionUID: config.Registry.ConnectionUID, InstallationID: config.InstallationID,
		Namespaces: config.Namespaces,
	})
}
