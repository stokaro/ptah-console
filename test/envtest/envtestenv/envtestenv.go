// Package envtestenv starts one kube-apiserver and etcd for an envtest
// package and stops them after its tests.
//
// No controller runs there, the operator's included, so what these suites
// prove is the API server's verdict: authentication, RBAC, the operator's
// CRD schemas. What the operator does with an object belongs in the kind
// suites.
package envtestenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/stokaro/ptah-console/internal/kube"
)

// RequireVariable makes a missing control plane fail instead of skip. CI and
// make test-envtest set it.
const RequireVariable = "PTAH_CONSOLE_REQUIRE_ENVTEST"

// Options choose what the control plane starts with.
type Options struct {
	// OperatorCRDs installs the operator's CRDs from the module the console
	// builds against.
	OperatorCRDs bool
}

// Plane is a running control plane.
type Plane struct {
	Env *envtest.Environment
	// Admin acts as the control plane's administrator, for arranging a test.
	// The code under test never gets it.
	Admin client.Client
}

// User adds an identity the API server authenticates, with its own client
// certificate, and returns its REST configuration.
func (p *Plane) User(name string, groups ...string) (*rest.Config, error) {
	user, err := p.Env.AddUser(envtest.User{Name: name, Groups: groups}, nil)
	if err != nil {
		return nil, fmt.Errorf("add user %s: %w", name, err)
	}
	return user.Config(), nil
}

// Run starts the control plane, runs the package's tests, stops it, and
// returns the exit code for TestMain. Without KUBEBUILDER_ASSETS it skips the
// package, and says so, unless RequireVariable is set.
func Run(m *testing.M, options Options, plane **Plane) int {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if os.Getenv(RequireVariable) == "1" {
			fmt.Fprintln(os.Stderr, "envtest: KUBEBUILDER_ASSETS is not set and "+RequireVariable+"=1; run make test-envtest")
			return 1
		}
		fmt.Fprintln(os.Stdout, "envtest: skipping this package: KUBEBUILDER_ASSETS is not set; run make test-envtest")
		return 0
	}
	env := &envtest.Environment{ErrorIfCRDPathMissing: true}
	if options.OperatorCRDs {
		dir, err := operatorCRDs()
		if err != nil {
			fmt.Fprintln(os.Stderr, "envtest:", err)
			return 1
		}
		env.CRDDirectoryPaths = []string{dir}
	}
	config, err := env.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "envtest: start the control plane:", err)
		return 1
	}
	defer func() {
		if err := env.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "envtest: stop the control plane:", err)
		}
	}()
	admin, err := kube.NewClient(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "envtest:", err)
		return 1
	}
	*plane = &Plane{Env: env, Admin: admin}
	return m.Run()
}

// operatorCRDs finds config/crd/bases in the operator module go.mod names.
func operatorCRDs() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/stokaro/ptah-operator").Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("locate the operator module: %w: %s", err, exit.Stderr)
		}
		return "", fmt.Errorf("locate the operator module: %w", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "config", "crd", "bases")
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("the operator module has no CRDs at %s: %w", dir, err)
	}
	return dir, nil
}
