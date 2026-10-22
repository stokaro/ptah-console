package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stokaro/ptah-console/internal/enrollment"
	"github.com/stokaro/ptah-console/internal/localkube"
)

func newCluster(stdin io.Reader, stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "cluster",
		Short: "Enroll a cluster in a hosted console",
		Args:  exactArgs(0),
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	command.AddCommand(newEnroll(stdin, stdout))
	return command
}

func newEnroll(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var kubeconfig, kubeContext, file, valuesPath string
	helper := enrollment.Helper{Namespace: enrollment.DefaultAgentNamespace, Release: enrollment.DefaultRelease, Out: stdout}
	var approved bool
	command := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll the cluster your kubeconfig context names, from the console's enrollment file",
		Long: "enroll runs on the cluster to enroll, with your own kubeconfig, which it never sends anywhere.\n" +
			"It shows what the agent will be installed with and granted, and the enrollment's fingerprint,\n" +
			"which the console showed too. With your approval it writes the agent's channel credential into\n" +
			"its Secret here, completes the enrollment on the console, and writes the agent chart's values\n" +
			"with the Helm command that installs it. Run it again with the same file to resume.",
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if kubeContext == "" {
				return fmt.Errorf("%w: --context is required: enroll never uses the kubeconfig's current context", errUsage)
			}
			if file == "" {
				return fmt.Errorf("%w: --enrollment-file is required", errUsage)
			}
			data, err := enrollment.ReadFile(file)
			if err != nil {
				return err
			}
			parsed, err := enrollment.Parse(data)
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			resolved, err := localkube.Resolve(kubeconfig, kubeContext)
			if err != nil {
				return err
			}
			helper.File, helper.Kube, helper.ContextName = parsed, resolved.Config, resolved.Context
			helper.ValuesPath = valuesPath
			if helper.ValuesPath == "" {
				helper.ValuesPath = parsed.Identity.ClusterName + "-agent-values.yaml"
			}
			if !approved {
				helper.Confirm = confirmByName(stdin, stdout, parsed.Identity.ClusterName)
			}
			return helper.Enroll(cmd.Context())
		},
	}
	flags := command.Flags()
	flags.StringVar(&kubeContext, "context", "", "the kubeconfig context of the cluster to enroll (required)")
	flags.StringVar(&kubeconfig, "kubeconfig", "", "the kubeconfig (default: KUBECONFIG, then ~/.kube/config)")
	flags.StringVar(&file, "enrollment-file", "", "the enrollment file the console gave (required)")
	flags.StringVar(&helper.Namespace, "namespace", enrollment.DefaultAgentNamespace, "the namespace the agent runs in")
	flags.StringVar(&helper.Release, "release", enrollment.DefaultRelease, "the agent's Helm release name")
	flags.StringVar(&valuesPath, "values", "", "where to write the agent chart's values (default: <cluster>-agent-values.yaml)")
	flags.BoolVar(&approved, "yes", false, "approve without asking, for automation that reviewed the enrollment itself")
	return command
}

// confirmByName shows the summary and asks for the cluster's name, so an
// approval is never one keystroke.
func confirmByName(stdin io.Reader, stdout io.Writer, name string) func(string) (bool, error) {
	return func(summary string) (bool, error) {
		fmt.Fprint(stdout, summary)
		fmt.Fprintf(stdout, "\nType the cluster's name, %s, to approve: ", name)
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && answer == "" {
			return false, nil
		}
		return strings.TrimSpace(answer) == name, nil
	}
}
