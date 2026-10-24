package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/stokaro/ptah-console/internal/audit"
	"github.com/stokaro/ptah-console/internal/hosted"
	"github.com/stokaro/ptah-console/web"
)

func newServe(stdout io.Writer) *cobra.Command {
	var options hosted.Options
	var kubeconfig string
	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the console for people who sign in with a Console account or an OIDC provider",
		Long: "serve runs the console beside the operator. People sign in with the accounts in the\n" +
			"configuration, and each account acts in Kubernetes as its own username, through the\n" +
			"console's ServiceAccount impersonating exactly that name. With oidc in the configuration\n" +
			"they sign in with the provider instead, and act with their own ID token, which the API\n" +
			"server has to trust. It serves HTTPS only: its own TLS, or plain HTTP behind a proxy you\n" +
			"declare terminates TLS.",
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if options.ConfigPath == "" {
				return fmt.Errorf("%w: --config is required, and --passwords with accounts", errUsage)
			}
			if kubeconfig != "" {
				config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
				if err != nil {
					return fmt.Errorf("read --kubeconfig: %w", err)
				}
				options.Kube = config
			}
			options.Assets = web.Assets()
			options.Logger = slog.Default()
			options.Audit = audit.New(os.Stderr)
			options.Stdout = stdout
			return hosted.Run(cmd.Context(), options)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.ConfigPath, "config", "", "the configuration file")
	flags.StringVar(&options.PasswordsPath, "passwords", "", "the passwords file, for a configuration with accounts")
	flags.StringVar(&options.OIDCClientSecretFile, "oidc-client-secret-file", "",
		"the console's client secret at the OIDC provider, for a configuration with oidc (default: a public client)")
	flags.StringVar(&options.Listen, "listen", ":8443", "the address to serve on")
	flags.StringVar(&options.TLSCertFile, "tls-cert-file", "", "the certificate to serve HTTPS with, read again when it changes")
	flags.StringVar(&options.TLSKeyFile, "tls-key-file", "", "the certificate's private key")
	flags.BoolVar(&options.TLSTerminatedUpstream, "tls-terminated-upstream", false,
		"serve plain HTTP, because a proxy in front of the console terminates TLS")
	flags.BoolVar(&options.ReadOnly, "read-only", false, "refuse every change the console could send, whatever an account may do")
	flags.StringVar(&options.AgentListen, "agent-listen", "",
		"serve the agents of the registry's external clusters on this address, apart from the browser's")
	flags.StringVar(&options.AgentTLSCertFile, "agent-tls-cert-file", "",
		"the agent listener's certificate, from the console's own CA, read again when it changes")
	flags.StringVar(&options.AgentTLSKeyFile, "agent-tls-key-file", "", "the agent listener certificate's private key")
	flags.StringVar(&kubeconfig, "kubeconfig", "",
		"the console's own identity, for running it outside a cluster (default: the Pod's ServiceAccount)")
	return command
}
