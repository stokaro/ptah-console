// Package hosted_test holds the hosted console's clients to the API server's
// verdict. The console acts as a user the API server authenticates by
// certificate, standing in for its ServiceAccount, with the one grant the
// chart gives that ServiceAccount: impersonate on users, limited by
// resourceNames to the accounts' usernames.
package hosted_test

import (
	"context"
	"os"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{}, &plane))
}

const (
	consoleUser   = "ptah-console-envtest"
	alexUsername  = "ptah-console:envtest:alex"
	samUsername   = "ptah-console:envtest:sam"
	impersonation = "ptah-console-envtest-impersonate"
	namespaces    = "ptah-console-envtest-namespaces"
)

// console is the console's own configuration, once its grants exist: the
// impersonation the chart gives it, and a right of its own no account has.
var console = sync.OnceValues(func() (*rest.Config, error) {
	ctx := context.Background()
	objects := []client.Object{
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: impersonation},
			Rules: []rbacv1.PolicyRule{{
				APIGroups:     []string{""},
				Resources:     []string{"users"},
				Verbs:         []string{"impersonate"},
				ResourceNames: []string{alexUsername, samUsername},
			}},
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: namespaces},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get"}}},
		},
	}
	for _, role := range []string{impersonation, namespaces} {
		objects = append(objects, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: role},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: consoleUser}},
		})
	}
	for _, object := range objects {
		if err := plane.Admin.Create(ctx, object); err != nil {
			return nil, err
		}
	}
	return plane.User(consoleUser)
})

func consoleConfig(t *testing.T) *rest.Config {
	t.Helper()
	config, err := console()
	qt.New(t).Assert(err, qt.IsNil)
	return config
}

func hostedClient(t *testing.T, account string) *kube.Client {
	t.Helper()
	c := qt.New(t)
	factory, err := kube.NewHostedFactory(consoleConfig(t), map[string]string{
		"alex": alexUsername,
		"sam":  samUsername,
	}, 1)
	c.Assert(err, qt.IsNil)
	hosted, err := factory.For(kube.Principal{
		Mode: kube.ModeHosted, AccountID: account, Context: kube.HostedConnection, Generation: 1,
	})
	c.Assert(err, qt.IsNil)
	return hosted
}

// Two accounts used at the same time each act as their own username, with
// no group the console added: the API server adds system:authenticated to
// every impersonated user, and that is all.
func TestTwoAccountsUsedTogetherEachActAsThemselves(t *testing.T) {
	t.Parallel()
	tests := []struct {
		account, username string
	}{
		{account: "alex", username: alexUsername},
		{account: "sam", username: samUsername},
	}
	for _, test := range tests {
		t.Run(test.account, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			hosted := hostedClient(t, test.account)
			seen := make(map[string]int)
			groups := make(map[string]int)

			for range 20 {
				identity, err := kube.WhoAmI(t.Context(), hosted)
				c.Assert(err, qt.IsNil)
				seen[identity.Username]++
				for _, group := range identity.Groups {
					groups[group]++
				}
			}

			c.Assert(seen, qt.DeepEquals, map[string]int{test.username: 20})
			c.Assert(groups, qt.DeepEquals, map[string]int{"system:authenticated": 20})
		})
	}
}

// What the console may do itself, an account may not: the account's client
// is the account, with nothing of the ServiceAccount's rights.
func TestAnAccountHasNoneOfTheConsolesOwnRights(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	itself, err := kube.NewClient(consoleConfig(t))
	c.Assert(err, qt.IsNil)
	alex := hostedClient(t, "alex")

	c.Assert(itself.Get(t.Context(), client.ObjectKey{Name: "default"}, &corev1.Namespace{}), qt.IsNil)
	err = alex.Get(t.Context(), client.ObjectKey{Name: "default"}, &corev1.Namespace{})
	c.Assert(apierrors.IsForbidden(err), qt.IsTrue, qt.Commentf("error: %v", err))
}

// The grant names users and nothing else, so the API server refuses every
// other identity the console's configuration could be pointed at.
func TestTheGrantImpersonatesExactlyTheAccounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		impersonation rest.ImpersonationConfig
		forbidden     bool
	}{
		{name: "an account", impersonation: rest.ImpersonationConfig{UserName: alexUsername}},
		{name: "a username no account has",
			impersonation: rest.ImpersonationConfig{UserName: "ptah-console:envtest:kim"}, forbidden: true},
		{name: "system:masters",
			impersonation: rest.ImpersonationConfig{UserName: "admin", Groups: []string{"system:masters"}}, forbidden: true},
		{name: "a ServiceAccount",
			impersonation: rest.ImpersonationConfig{UserName: "system:serviceaccount:kube-system:default"}, forbidden: true},
		{name: "an account with a group",
			impersonation: rest.ImpersonationConfig{UserName: alexUsername, Groups: []string{"ops"}}, forbidden: true},
		{name: "an account with a UID",
			impersonation: rest.ImpersonationConfig{UserName: alexUsername, UID: "1234"}, forbidden: true},
		{name: "an account with an extra",
			impersonation: rest.ImpersonationConfig{UserName: alexUsername, Extra: map[string][]string{"scopes": {"all"}}},
			forbidden:     true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			config := rest.CopyConfig(consoleConfig(t))
			config.Impersonate = test.impersonation
			impersonating, err := kube.NewClient(config)
			c.Assert(err, qt.IsNil)

			_, err = kube.WhoAmI(t.Context(), impersonating)

			c.Assert(apierrors.IsForbidden(err), qt.Equals, test.forbidden, qt.Commentf("error: %v", err))
		})
	}
}
