package cli

import (
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/local"
	"github.com/stokaro/ptah-console/web"
)

func newLocal(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var options local.Options
	var noOpen bool
	command := &cobra.Command{
		Use:   "local",
		Short: "Run the console on this machine with your own kubeconfig",
		Long: "local serves the console on a loopback address and acts with one kubeconfig context,\n" +
			"resolved once at start. Nothing is installed in the cluster. The browser signs in with\n" +
			"a link that works once, for one minute; press Enter for a new one.",
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			options.Assets = web.Assets()
			options.Logger = slog.Default()
			options.Audit = audit.New(os.Stderr)
			options.Stdout = stdout
			options.Stdin = stdin
			file, isFile := stdin.(*os.File)
			options.Interactive = isFile && isTerminal(file)
			if !noOpen {
				options.Open = local.OpenBrowser
			}
			return local.Run(cmd.Context(), options)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Kubeconfig, "kubeconfig", "", "kubeconfig file (default: KUBECONFIG, then ~/.kube/config)")
	flags.StringVar(&options.Context, "context", "", "kubeconfig context to act with (default: the current one, pinned at start)")
	flags.StringSliceVarP(&options.Namespaces, "namespace", "n", nil,
		"namespace to show; repeat for more (default: the context's namespace, or every namespace you may list)")
	flags.BoolVar(&options.ReadOnly, "read-only", false, "refuse every change the console could send, whatever the identity may do")
	flags.IntVar(&options.Port, "port", 0, "loopback port (default: a free one)")
	flags.BoolVar(&noOpen, "no-open", false, "print the sign-in link instead of opening the browser")
	flags.BoolVar(&options.TrustExecPlugin, "trust-exec-plugin", false,
		"agree without asking to run the context's credential plugin, and remember it")
	return command
}

// isTerminal reports whether f is a terminal a person can answer from.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
