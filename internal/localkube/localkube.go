// Package localkube resolves the kubeconfig context ptah-console local acts
// with, once, at start.
//
// The context is pinned by name when it is resolved; nothing reads the
// kubeconfig's current-context afterwards. A context that runs a credential
// plugin says so, with a digest of exactly what it runs, so the caller can
// ask the person before the plugin first runs. A context that uses a legacy
// auth-provider is refused: client-go writes refreshed tokens from those back
// into the kubeconfig, and the console never rewrites it.
package localkube

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Resolved is a context, pinned.
type Resolved struct {
	// Kubeconfig is the file the context came from, or the files the default
	// loading rules merged, joined by the path list separator.
	Kubeconfig string
	Context    string
	Config     *rest.Config
	// ExecPlugin is set when the context runs a credential plugin.
	ExecPlugin *ExecPlugin
	// Impersonates is set when the kubeconfig itself asks to impersonate.
	Impersonates string
	// InsecureTLS is true when the kubeconfig turns off verification of the
	// API server's certificate.
	InsecureTLS bool
}

// ExecPlugin is the credential plugin a context runs.
type ExecPlugin struct {
	Command string
	Args    []string
	// Digest covers the command, the arguments, the environment and the API
	// version, so any change to what runs is a new question.
	Digest string
}

// Resolve loads the kubeconfig and pins the context. An empty kubeconfig
// uses the default loading rules (KUBECONFIG, then ~/.kube/config). An empty
// context pins the kubeconfig's current-context as it is now.
func Resolve(kubeconfig, context string) (Resolved, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	raw, err := rules.Load()
	if err != nil {
		return Resolved{}, fmt.Errorf("localkube: load the kubeconfig: %w", err)
	}
	if context == "" {
		context = raw.CurrentContext
	}
	if context == "" {
		return Resolved{}, errors.New("localkube: the kubeconfig sets no current-context; name one with --context")
	}
	kubeContext, ok := raw.Contexts[context]
	if !ok {
		return Resolved{}, fmt.Errorf("localkube: the kubeconfig has no context %q", context)
	}
	authInfo := raw.AuthInfos[kubeContext.AuthInfo]
	if authInfo != nil && authInfo.AuthProvider != nil {
		return Resolved{}, fmt.Errorf(
			"localkube: context %q authenticates with the %q auth-provider, which writes refreshed tokens back into the kubeconfig; "+
				"use an exec credential plugin instead", context, authInfo.AuthProvider.Name)
	}

	// The loaded config is handed over as is, so client-go reads nothing
	// again and has no file to persist to.
	config, err := clientcmd.NewNonInteractiveClientConfig(*raw, context, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return Resolved{}, fmt.Errorf("localkube: build the client configuration for %q: %w", context, err)
	}

	resolved := Resolved{
		Kubeconfig:   source(rules),
		Context:      context,
		Config:       config,
		Impersonates: config.Impersonate.UserName,
		InsecureTLS:  config.Insecure,
	}
	if authInfo != nil && authInfo.Exec != nil {
		plugin, err := execPlugin(authInfo.Exec)
		if err != nil {
			return Resolved{}, err
		}
		resolved.ExecPlugin = plugin
	}
	return resolved, nil
}

func source(rules *clientcmd.ClientConfigLoadingRules) string {
	if rules.ExplicitPath != "" {
		if abs, err := filepath.Abs(rules.ExplicitPath); err == nil {
			return abs
		}
		return rules.ExplicitPath
	}
	return filepath.Join(rules.GetLoadingPrecedence()...)
}

func execPlugin(exec *clientcmdapi.ExecConfig) (*ExecPlugin, error) {
	described, err := json.Marshal(struct {
		Command    string                    `json:"command"`
		Args       []string                  `json:"args"`
		Env        []clientcmdapi.ExecEnvVar `json:"env"`
		APIVersion string                    `json:"apiVersion"`
	}{exec.Command, exec.Args, exec.Env, exec.APIVersion})
	if err != nil {
		return nil, fmt.Errorf("localkube: describe the credential plugin: %w", err)
	}
	sum := sha256.Sum256(described)
	return &ExecPlugin{
		Command: exec.Command,
		Args:    append([]string(nil), exec.Args...),
		Digest:  "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}
