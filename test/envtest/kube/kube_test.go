package kube_test

import (
	"os"
	"testing"

	qt "github.com/frankban/quicktest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stokaro/ptah-console/internal/kube"
	"github.com/stokaro/ptah-console/test/envtest/envtestenv"
)

var plane *envtestenv.Plane

func TestMain(m *testing.M) {
	os.Exit(envtestenv.Run(m, envtestenv.Options{}, &plane))
}

// localClient builds the client a person running ptah-console local would
// get, for a user the API server authenticates by certificate.
func localClient(t *testing.T, user string) *kube.Client {
	t.Helper()
	c := qt.New(t)
	config, err := plane.User(user)
	c.Assert(err, qt.IsNil)
	principal := kube.Principal{Mode: kube.ModeLocal, Context: "envtest-" + user, Generation: 1}
	factory, err := kube.NewLocalFactory(principal, config)
	c.Assert(err, qt.IsNil)
	client, err := factory.For(principal)
	c.Assert(err, qt.IsNil)
	return client
}

// Two people's clients used at the same time each act as their own person.
// A shared configuration rewritten per request would hand one of them the
// other's identity under load.
func TestTwoIdentitiesUsedTogetherEachActAsThemselves(t *testing.T) {
	t.Parallel()
	users := []string{"alice", "bob"}
	clients := make(map[string]*kube.Client, len(users))
	for _, user := range users {
		clients[user] = localClient(t, user)
	}
	for _, user := range users {
		t.Run(user, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			seen := make(map[string]int)
			for range 20 {
				identity, err := kube.WhoAmI(t.Context(), clients[user])
				c.Assert(err, qt.IsNil)
				seen[identity.Username]++
			}

			c.Assert(seen, qt.DeepEquals, map[string]int{user: 20})
		})
	}
}

// The hint is the API server's answer for this identity: the same question
// gets a different answer for someone the RBAC says something else about.
func TestCanAnswersForTheIdentityThatAsks(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	c.Assert(plane.Admin.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "application-hints"}}), qt.IsNil)
	c.Assert(plane.Admin.Create(t.Context(), &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: "application-hints", Name: "plan-reviewer"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"operator.ptah.run"},
			Resources: []string{"ptahschemas", "ptahschemaplans", "ptahschemaplanchunks"},
			Verbs:     []string{"get"},
		}},
	}), qt.IsNil)
	c.Assert(plane.Admin.Create(t.Context(), &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: "application-hints", Name: "plan-reviewer"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "plan-reviewer"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "reviewer"}},
	}), qt.IsNil)
	reviewer := localClient(t, "reviewer")
	stranger := localClient(t, "stranger")
	checks := []kube.Check{
		{Namespace: "application-hints", Verb: "get", Group: "operator.ptah.run", Resource: "ptahschemaplanchunks"},
		{Namespace: "application-hints", Verb: "list", Group: "operator.ptah.run", Resource: "ptahschemas"},
		{Namespace: "default", Verb: "get", Group: "operator.ptah.run", Resource: "ptahschemaplanchunks"},
	}

	reviewerHints := kube.Can(t.Context(), reviewer, checks...)
	strangerHints := kube.Can(t.Context(), stranger, checks...)

	c.Assert(answers(reviewerHints), qt.DeepEquals, []kube.Answer{kube.Allowed, kube.Denied, kube.Denied})
	c.Assert(answers(strangerHints), qt.DeepEquals, []kube.Answer{kube.Denied, kube.Denied, kube.Denied})
}

func answers(hints []kube.Hint) []kube.Answer {
	out := make([]kube.Answer, 0, len(hints))
	for _, hint := range hints {
		out = append(out, hint.Answer)
	}
	return out
}
