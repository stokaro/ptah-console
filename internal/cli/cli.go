// Package cli is the ptah-console command line.
//
// Run is the whole program apart from the process: it takes arguments and the
// three standard streams and returns the exit code, so a test drives it the
// way a shell does.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
)

// Exit codes.
const (
	ExitOK     = 0
	ExitFailed = 1
	ExitUsage  = 2
)

// errUsage marks a mistake in how the command was called.
var errUsage = errors.New("usage")

// Run executes one invocation and returns its exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(stdin, stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "ptah-console: %v\n", err)
		return ExitUsage
	default:
		fmt.Fprintf(stderr, "ptah-console: %v\n", err)
		return ExitFailed
	}
}

func newRoot(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	var logLevel string
	root := &cobra.Command{
		Use:   "ptah-console",
		Short: "Web console for Ptah Operator",
		Long: "ptah-console shows what Ptah Operator is doing with your schemas and migrations,\n" +
			"and lets you approve the exact plan you reviewed under your own Kubernetes identity.",
		SilenceErrors: true,
		SilenceUsage:  true,
		// Cobra reports an unknown subcommand with an untyped error. Taking
		// the arguments here lets it be a usage mistake like any other.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%w: unknown command %q for %q", errUsage, args[0], cmd.CommandPath())
			}
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			level, err := parseLevel(logLevel)
			if err != nil {
				return err
			}
			slog.SetDefault(slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level})))
			return nil
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %w", errUsage, err)
	})
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn or error")
	root.AddCommand(
		newVersion(stdout),
		newLocal(stdin, stdout),
		newServe(stdout),
		newAccounts(stdin, stdout, stderr),
		newConfig(stdout),
	)
	return root
}

func parseLevel(text string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(text)); err != nil {
		return 0, fmt.Errorf("%w: --log-level %q is not one of debug, info, warn or error", errUsage, text)
	}
	return level, nil
}

// exactArgs is cobra.ExactArgs that reports a usage mistake.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return fmt.Errorf("%w: %w", errUsage, err)
		}
		return nil
	}
}
