package hosted_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
	"github.com/stokaro/ptah-console/internal/auth/accounts"
	"github.com/stokaro/ptah-console/internal/hosted"
	"github.com/stokaro/ptah-console/internal/registry"
)

const (
	registryNS     = "ptah-console-envtest-registry"
	thisConnection = "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f"
)

// A console with the registry on starts only against this cluster's entry
// as its configuration describes it, read with its own identity; told of
// another connection, it refuses before it listens.
func TestTheConsoleStartsOnlyAgainstItsOwnEntry(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := t.Context()
	c.Assert(plane.Admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: registryNS}}), qt.IsNil)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: registryNS, Name: "housekeeping"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{consolev1alpha1.GroupVersion.Group}, Resources: []string{"consoleclusters"}, Verbs: []string{"get"},
		}},
	}
	c.Assert(plane.Admin.Create(ctx, role), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: registryNS, Name: "housekeeping"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(ctx, &consolev1alpha1.ConsoleCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: registryNS, Name: "this-cluster"},
		Spec: consolev1alpha1.ConsoleClusterSpec{
			ConnectionUID: thisConnection, Profile: consolev1alpha1.ProfileCoLocated, InstallationID: "envtest",
			Namespaces: []string{"storefront"}, ConfigurationOwner: consolev1alpha1.OwnerGitOps,
		},
	}), qt.IsNil)

	listened, err := runWithRegistry(t, thisConnection)
	c.Assert(err, qt.IsNil)
	c.Assert(listened, qt.IsTrue)

	listened, err = runWithRegistry(t, "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d")
	c.Assert(err, qt.ErrorIs, registry.ErrThisClusterDisagrees)
	c.Assert(listened, qt.IsFalse)
}

// runWithRegistry runs a hosted console told that this cluster's entry has
// connectionUID, stops it once it listens, and reports whether it did.
func runWithRegistry(t *testing.T, connectionUID string) (bool, error) {
	t.Helper()
	c := qt.New(t)
	dir := t.TempDir()
	hash, err := accounts.Hash([]byte("alex-secret"), accounts.MinCost)
	c.Assert(err, qt.IsNil)
	configPath, passwordsPath := filepath.Join(dir, "configuration.yaml"), filepath.Join(dir, "passwords")
	c.Assert(os.WriteFile(configPath, []byte(`apiVersion: console.ptah.run/v1alpha1
kind: HostedConfiguration
installationID: envtest
publicURL: https://console.envtest
namespaces: [storefront]
accounts:
  - id: alex
    login: alex@example.com
registry:
  namespace: `+registryNS+`
  thisCluster: this-cluster
  connectionUID: `+connectionUID+`
`), 0o600), qt.IsNil)
	c.Assert(os.WriteFile(passwordsPath, []byte("alex:"+string(hash)+"\n"), 0o600), qt.IsNil)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listened := false
	err = hosted.Run(ctx, hosted.Options{
		ConfigPath: configPath, PasswordsPath: passwordsPath, Listen: "127.0.0.1:0",
		TLSTerminatedUpstream: true, Kube: consoleConfig(t),
		Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
		Listening: func(net.Addr) {
			listened = true
			cancel()
		},
	})
	return listened, err
}
