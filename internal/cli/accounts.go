package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

// maxPasswordInput bounds what is read from a pipe. Anything near it is not
// one password, and reading it whole would take whatever the pipe holds.
const maxPasswordInput = 4096

func newAccounts(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	command := group(&cobra.Command{
		Use:   "accounts",
		Short: "Work with the accounts of a hosted console",
	})
	cost := accounts.DefaultCost
	hash := &cobra.Command{
		Use:   "hash",
		Short: "Print the bcrypt hash of a password, for the passwords file",
		Long: "hash reads a password and prints its bcrypt hash. From a terminal it asks twice and\n" +
			"does not echo; otherwise it reads one line from standard input. It never takes the\n" +
			"password as an argument, which would leave it in shell history and the process list.\n\n" +
			"A line of the passwords file is the account ID, a colon and this hash.",
		Args: exactArgs(0),
		RunE: func(*cobra.Command, []string) error {
			password, err := readPassword(stdin, stderr)
			if err != nil {
				return err
			}
			hashed, err := accounts.Hash(password, cost)
			clear(password)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "%s\n", hashed)
			return err
		},
	}
	hash.Flags().IntVar(&cost, "cost", accounts.DefaultCost,
		fmt.Sprintf("bcrypt cost, %d or more", accounts.MinCost))
	command.AddCommand(hash)
	return command
}

// readPassword asks a person at a terminal twice, or reads one line from
// anything else.
func readPassword(stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if file, ok := stdin.(*os.File); ok && isTerminal(file) {
		return promptPassword(file, stderr)
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxPasswordInput+1))
	if err != nil {
		return nil, fmt.Errorf("read the password: %w", err)
	}
	if len(data) > maxPasswordInput {
		return nil, errors.New("standard input holds more than a password")
	}
	data = bytes.TrimSuffix(bytes.TrimSuffix(data, []byte("\n")), []byte("\r"))
	if bytes.ContainsAny(data, "\r\n") {
		return nil, errors.New("standard input holds more than one line; give the password alone")
	}
	return data, nil
}

func promptPassword(file *os.File, stderr io.Writer) ([]byte, error) {
	fd := int(file.Fd()) // #nosec G115 -- a file descriptor fits in an int on every platform Go supports.
	read := func(prompt string) ([]byte, error) {
		_, _ = fmt.Fprint(stderr, prompt)
		password, err := term.ReadPassword(fd)
		_, _ = fmt.Fprintln(stderr)
		if err != nil {
			return nil, fmt.Errorf("read the password: %w", err)
		}
		return password, nil
	}
	first, err := read("Password: ")
	if err != nil {
		return nil, err
	}
	second, err := read("Again: ")
	if err != nil {
		return nil, err
	}
	same := bytes.Equal(first, second)
	clear(second)
	if !same {
		clear(first)
		return nil, errors.New("the two passwords differ")
	}
	return first, nil
}

func newConfig(stdout io.Writer) *cobra.Command {
	command := group(&cobra.Command{
		Use:   "config",
		Short: "Work with the configuration of a hosted console",
	})
	var configPath, passwordsPath string
	validate := &cobra.Command{
		Use:   "validate",
		Short: "Check a hosted configuration and its passwords file",
		Long: "validate reads the configuration and the passwords file the way the server does, and\n" +
			"reports every problem in them. For a valid pair it prints the Kubernetes username\n" +
			"each account acts as, which is what RBAC bindings name.",
		Args: exactArgs(0),
		RunE: func(*cobra.Command, []string) error {
			if configPath == "" || passwordsPath == "" {
				return fmt.Errorf("%w: --config and --passwords are both required", errUsage)
			}
			config, _, err := accounts.Load(configPath, passwordsPath)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(stdout, "Valid for installation %s, in namespaces %s.\nThe accounts act as:\n",
				config.InstallationID, strings.Join(config.Namespaces, ", ")); err != nil {
				return err
			}
			for _, account := range config.Accounts {
				if _, err := fmt.Fprintf(stdout, "  %s  (%s)\n", config.Username(account), account.Login); err != nil {
					return err
				}
			}
			return nil
		},
	}
	flags := validate.Flags()
	flags.StringVar(&configPath, "config", "", "the configuration file")
	flags.StringVar(&passwordsPath, "passwords", "", "the passwords file")
	command.AddCommand(validate)
	return command
}

// group makes a command that only holds subcommands, and reports an unknown
// one as a usage mistake, as the root does.
func group(command *cobra.Command) *cobra.Command {
	command.Args = cobra.ArbitraryArgs
	command.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("%w: unknown command %q for %q", errUsage, args[0], cmd.CommandPath())
		}
		return cmd.Help()
	}
	return command
}
