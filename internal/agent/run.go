package agent

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"k8s.io/client-go/rest"

	"github.com/stokaro/ptah-console/internal/audit"
)

// DefaultReloadInterval is how often the allowlist file is read for a change.
// Kubernetes updates a mounted ConfigMap within about a minute, so this adds
// little to that.
const DefaultReloadInterval = 10 * time.Second

// Options are what an installation gives the agent.
type Options struct {
	ConfigPath string
	// AccountsPath is the allowlist, read again when it changes.
	AccountsPath string
	// CAPath is the console's own CA, in PEM.
	CAPath string
	// CredentialPath is the channel credential, read again on every attempt
	// to open the channel.
	CredentialPath string
	// Kube is the agent's own identity; nil means the Pod's ServiceAccount.
	Kube           *rest.Config
	Version        string
	Logger         *slog.Logger
	Audit          *audit.Logger
	ReloadInterval time.Duration
}

// Main runs the agent until ctx ends.
func Main(ctx context.Context, options Options) error {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	data, err := ReadFile(options.ConfigPath)
	if err != nil {
		return err
	}
	config, err := ParseConfiguration(data)
	if err != nil {
		return fmt.Errorf("%s: %w", options.ConfigPath, err)
	}
	pem, err := ReadFile(options.CAPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return fmt.Errorf("%s: holds no certificate", options.CAPath)
	}
	base := options.Kube
	if base == nil {
		if base, err = rest.InClusterConfig(); err != nil {
			return fmt.Errorf("read the agent's own Kubernetes identity: %w", err)
		}
	}
	factory, err := NewFactory(base, config)
	if err != nil {
		return err
	}
	accounts, err := ReadFile(options.AccountsPath)
	if err != nil {
		return err
	}
	if err := setAccounts(factory, accounts); err != nil {
		return fmt.Errorf("%s: %w", options.AccountsPath, err)
	}
	dispatcher := NewDispatcher(factory, options.Audit, nil)
	interval := options.ReloadInterval
	if interval <= 0 {
		interval = DefaultReloadInterval
	}
	go Reload(ctx, options.AccountsPath, accounts, interval, factory, dispatcher, logger)

	logger.Info("the agent serves its connection",
		"connection", config.Connection.UID, "generation", config.Connection.Generation,
		"console", config.Console.Address, "namespaces", config.Namespaces)
	return Run(ctx, ChannelOptions{
		Config: config, RootCAs: roots, Version: options.Version, Logger: logger,
		Credential: func() (string, error) {
			data, err := ReadFile(options.CredentialPath)
			return strings.TrimSpace(string(data)), err
		},
	}, dispatcher)
}

// Reload reads the allowlist every interval and takes a changed one. A file
// that does not parse leaves the list in force as it was.
func Reload(ctx context.Context, path string, last []byte, interval time.Duration, factory *Factory, dispatcher *Dispatcher, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		data, err := ReadFile(path)
		if err != nil {
			logger.Error("the allowlist could not be read; the one in force stays", "error", err)
			continue
		}
		if bytes.Equal(data, last) {
			continue
		}
		if err := setAccounts(factory, data); err != nil {
			logger.Error("the changed allowlist was refused; the one in force stays", "error", err)
			continue
		}
		last = data
		dispatcher.Revoke()
		logger.Info("the allowlist changed")
	}
}

func setAccounts(factory *Factory, data []byte) error {
	list, err := ParseAccounts(data)
	if err != nil {
		return err
	}
	if err := factory.Set(list); err != nil {
		return errors.Join(errors.New("the allowlist was not taken"), err)
	}
	return nil
}
