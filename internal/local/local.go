// Package local runs ptah-console local: the console on the person's own
// machine, acting with their own kubeconfig, on a loopback listener.
//
// Nothing is installed in a cluster and no account is configured. The
// browser is signed in with a single-use launch link; whoever does not have
// the link, including another page in the same browser, gets nothing.
package local

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/stokaro/ptah-console/internal/api"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/auth/launch"
	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/internal/localkube"
	"github.com/stokaro/ptah-console/internal/localstate"
	"github.com/stokaro/ptah-console/internal/server"
)

// Options configure one run.
type Options struct {
	Kubeconfig string
	Context    string
	// Namespaces are the namespaces offered. Empty offers the context's
	// namespace, or the namespaces the identity may list.
	Namespaces []string
	ReadOnly   bool
	// Port is the loopback port; zero picks a free one.
	Port int
	// TrustExecPlugin agrees, without asking, to run the context's credential
	// plugin, and records that.
	TrustExecPlugin bool
	// StatePath is the state file; empty uses localstate.DefaultPath.
	StatePath string

	Assets fs.FS
	Logger *slog.Logger
	// Audit receives one line per action. Nil discards them.
	Audit *audit.Logger

	// Stdin is read for answers and for Enter, when Interactive is true.
	Stdin       io.Reader
	Interactive bool
	Stdout      io.Writer

	// Open opens a URL in the browser. Nil prints the URL only.
	Open func(url string) error
}

// Run serves the console until ctx ends.
func Run(ctx context.Context, options Options) error {
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	if options.Audit == nil {
		options.Audit = audit.Discard()
	}
	resolved, err := localkube.Resolve(options.Kubeconfig, options.Context)
	if err != nil {
		return err
	}
	if err := trustPlugin(resolved, options); err != nil {
		return err
	}

	principal := kube.Principal{Mode: kube.ModeLocal, Context: resolved.Context, Generation: 1}
	factory, err := kube.NewLocalFactory(principal, resolved.Config)
	if err != nil {
		return err
	}

	listeners, port, err := listen(options.Port, options.Logger)
	if err != nil {
		return err
	}
	origins := origins(listeners, port)

	scope := api.Scope{Candidates: options.Namespaces}
	if len(scope.Candidates) == 0 && resolved.Namespace != "" {
		scope.Candidates = []string{resolved.Namespace}
	}

	store := session.NewStore(session.Options{})
	launcher := launch.New(launch.DefaultTTL)
	cookies := session.Cookies{Secure: false}
	sessions := api.NewLocalSession(store, cookies, launcher, principal, factory, api.SessionFacts{
		Mode:         consolev1.Mode_MODE_LOCAL,
		ReadOnly:     options.ReadOnly,
		Impersonates: resolved.Impersonates,
		InsecureTLS:  resolved.InsecureTLS,
	}, options.Audit)
	interceptors := []connect.ServerInterceptor{
		session.Interceptor(store, cookies,
			consolev1connect.InfoServiceGetInfoProcedure,
			consolev1connect.SessionServiceExchangeLaunchTokenProcedure),
	}
	if options.ReadOnly {
		interceptors = append(interceptors, api.ReadOnly(
			consolev1connect.SessionServiceExchangeLaunchTokenProcedure,
			consolev1connect.SessionServiceLogoutProcedure))
	}
	handler, err := server.New(server.Options{
		Mode:         consolev1.Mode_MODE_LOCAL,
		ReadOnly:     options.ReadOnly,
		Assets:       options.Assets,
		Origins:      origins,
		Logger:       options.Logger,
		Interceptors: interceptors,
		Services: []func(*connect.Server){func(rpc *connect.Server) {
			api.Register(rpc, sessions, factory, scope, options.Audit)
		}},
	})
	if err != nil {
		closeAll(listeners)
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go store.Run(ctx, time.Minute)
	served := server.Serve(ctx, server.NewHTTPServer(handler, nil, options.Logger), listeners)

	fmt.Fprintf(options.Stdout, "Ptah Console for context %q is at %s\n", resolved.Context, origins[0])
	if resolved.InsecureTLS {
		fmt.Fprintf(options.Stdout, "Warning: context %q does not verify the API server's certificate.\n", resolved.Context)
	}
	if err := offer(launcher, origins[0], options); err != nil {
		return err
	}
	if options.Interactive {
		go relaunchOnEnter(ctx, launcher, origins[0], options)
	}
	return <-served
}

// trustPlugin asks once before a context's credential plugin first runs: the
// kubeconfig is a file anyone may have handed the person, and the plugin is a
// program it runs.
func trustPlugin(resolved localkube.Resolved, options Options) error {
	if resolved.ExecPlugin == nil {
		return nil
	}
	path := options.StatePath
	if path == "" {
		var err error
		if path, err = localstate.DefaultPath(); err != nil {
			return err
		}
	}
	state, err := localstate.Load(path)
	if err != nil {
		return err
	}
	plugin := localstate.TrustedPlugin{
		Kubeconfig: resolved.Kubeconfig,
		Context:    resolved.Context,
		Digest:     resolved.ExecPlugin.Digest,
	}
	if state.Trusts(plugin) {
		return nil
	}
	command := strings.Join(append([]string{resolved.ExecPlugin.Command}, resolved.ExecPlugin.Args...), " ")
	if !options.TrustExecPlugin {
		if !options.Interactive {
			return fmt.Errorf("context %q runs %q to get credentials; run ptah-console local in a terminal to agree to it, or pass --trust-exec-plugin",
				resolved.Context, command)
		}
		fmt.Fprintf(options.Stdout, "Context %q runs this command to get credentials:\n  %s\nRun it? [y/N] ", resolved.Context, command)
		answer, _ := bufio.NewReader(options.Stdin).ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
			return errors.New("the credential plugin was not agreed to")
		}
	}
	plugin.TrustedAt = time.Now().UTC()
	state.Trust(plugin)
	return localstate.Save(path, state)
}

// listen binds the IPv4 and IPv6 loopback addresses on one port. IPv6 is
// optional: a machine without it still gets a console.
func listen(port int, logger *slog.Logger) ([]net.Listener, int, error) {
	v4, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, 0, fmt.Errorf("listen on 127.0.0.1: %w", err)
	}
	actual := v4.Addr().(*net.TCPAddr).Port
	v6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(actual)))
	if err != nil {
		logger.Warn("no IPv6 loopback listener", "error", err)
		return []net.Listener{v4}, actual, nil
	}
	return []net.Listener{v4, v6}, actual, nil
}

// origins are the addresses the console answers to: the loopback literals it
// listens on, and localhost, which resolves to them and to nothing else.
func origins(listeners []net.Listener, port int) []string {
	out := []string{"http://127.0.0.1:" + strconv.Itoa(port)}
	if len(listeners) > 1 {
		out = append(out, "http://[::1]:"+strconv.Itoa(port))
	}
	return append(out, "http://localhost:"+strconv.Itoa(port))
}

// offer issues a launch link and opens it, or prints it.
func offer(launcher *launch.Launcher, origin string, options Options) error {
	token, err := launcher.Issue()
	if err != nil {
		return err
	}
	// The token goes in the fragment: a browser never sends it to a server.
	link := origin + "/#t=" + token
	if options.Open != nil {
		if err := options.Open(link); err == nil {
			fmt.Fprintln(options.Stdout, "Opened the browser. The link works once, for one minute.")
			return nil
		}
	}
	fmt.Fprintf(options.Stdout, "Open this link; it works once, for one minute:\n  %s\n", link)
	return nil
}

func relaunchOnEnter(ctx context.Context, launcher *launch.Launcher, origin string, options Options) {
	fmt.Fprintln(options.Stdout, "Press Enter for a new link.")
	lines := bufio.NewScanner(options.Stdin)
	for lines.Scan() {
		if ctx.Err() != nil {
			return
		}
		if err := offer(launcher, origin, options); err != nil {
			options.Logger.Warn("could not issue a launch link", "error", err)
		}
	}
}

func closeAll(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
