// Command ptah-console-agent runs in a cluster a hosted console does not run
// in. It opens a channel to the console, outbound, and serves the console's
// typed requests as the people they name, through its own allowlist.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/stokaro/ptah-console/internal/agent"
	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/buildinfo"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ptah-console-agent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options agent.Options
	var kubeconfig string
	var version bool
	flags.StringVar(&options.ConfigPath, "config", "", "the agent configuration file")
	flags.StringVar(&options.AccountsPath, "accounts", "", "the allowlist file, read again when it changes")
	flags.StringVar(&options.CAPath, "console-ca", "", "the console's own CA, in PEM")
	flags.StringVar(&options.CredentialPath, "credential", "", "the channel credential, read again on every attempt")
	flags.StringVar(&kubeconfig, "kubeconfig", "", "the agent's own identity, outside a cluster (default: the Pod's ServiceAccount)")
	flags.BoolVar(&version, "version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	info := buildinfo.Read()
	if version {
		_, _ = fmt.Fprintln(stdout, info.Version, info.Commit)
		return 0
	}
	if flags.NArg() > 0 || options.ConfigPath == "" || options.AccountsPath == "" || options.CAPath == "" || options.CredentialPath == "" {
		_, _ = fmt.Fprintln(stderr, "ptah-console-agent: --config, --accounts, --console-ca and --credential are required, and nothing else")
		return 2
	}
	if kubeconfig != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ptah-console-agent: read --kubeconfig:", err)
			return 1
		}
		options.Kube = config
	}
	options.Version = info.Version
	options.Logger = slog.New(slog.NewJSONHandler(stderr, nil))
	options.Audit = audit.New(stderr)
	if err := agent.Main(ctx, options); err != nil {
		options.Logger.Error("the agent stopped", "error", err)
		return 1
	}
	return 0
}
