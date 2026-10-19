package kube_test

import (
	"context"
	"errors"
	"testing"

	qt "github.com/frankban/quicktest"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stokaro/ptah-console/internal/kube"
)

func TestPrincipalValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		principal kube.Principal
		refused   bool
	}{
		{name: "local", principal: kube.Principal{Mode: kube.ModeLocal, Context: "production"}},
		{name: "hosted", principal: kube.Principal{Mode: kube.ModeHosted, AccountID: "a-1", Context: "this-cluster"}},
		{name: "local with an account", principal: kube.Principal{Mode: kube.ModeLocal, AccountID: "a-1", Context: "production"}, refused: true},
		{name: "hosted without an account", principal: kube.Principal{Mode: kube.ModeHosted, Context: "this-cluster"}, refused: true},
		{name: "no context", principal: kube.Principal{Mode: kube.ModeLocal}, refused: true},
		{name: "no mode", principal: kube.Principal{Context: "production"}, refused: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			err := test.principal.Validate()

			c.Assert(err != nil, qt.Equals, test.refused, qt.Commentf("error: %v", err))
		})
	}
}

// A client keeps its own copy of the configuration: whatever happens to the
// configuration it was built from afterwards, the identity does not move.
func TestNewClientKeepsItsOwnConfiguration(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	config := &rest.Config{Host: "https://127.0.0.1:6443", BearerToken: "alice"}

	built, err := kube.NewClient(config)
	c.Assert(err, qt.IsNil)
	config.BearerToken = "bob"
	config.Impersonate.UserName = "system:admin"

	c.Assert(built.Config.BearerToken, qt.Equals, "alice")
	c.Assert(built.Config.Impersonate.UserName, qt.Equals, "")
}

// The local factory acts for the principal it was started with and nobody
// else, a newer generation of the same context included: a decision made
// under the old credentials must not go out under the new ones.
func TestLocalFactoryActsOnlyForItsPrincipal(t *testing.T) {
	t.Parallel()
	started := kube.Principal{Mode: kube.ModeLocal, Context: "production", Generation: 1}
	tests := []struct {
		name      string
		principal kube.Principal
		refused   bool
	}{
		{name: "the principal it was started with", principal: started},
		{name: "another context", principal: kube.Principal{Mode: kube.ModeLocal, Context: "staging", Generation: 1}, refused: true},
		{name: "a newer generation", principal: kube.Principal{Mode: kube.ModeLocal, Context: "production", Generation: 2}, refused: true},
		{name: "a hosted principal", principal: kube.Principal{Mode: kube.ModeHosted, AccountID: "a-1", Context: "production", Generation: 1}, refused: true},
	}
	factory, err := kube.NewLocalFactory(started, &rest.Config{Host: "https://127.0.0.1:6443"})
	qt.New(t).Assert(err, qt.IsNil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			built, err := factory.For(test.principal)

			c.Assert(err != nil, qt.Equals, test.refused, qt.Commentf("error: %v", err))
			c.Assert(built == nil, qt.Equals, test.refused)
		})
	}
}

func TestNewLocalFactoryRefusesAHostedPrincipal(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	factory, err := kube.NewLocalFactory(
		kube.Principal{Mode: kube.ModeHosted, AccountID: "a-1", Context: "this-cluster"},
		&rest.Config{Host: "https://127.0.0.1:6443"})

	c.Assert(err, qt.ErrorMatches, `.*cannot act for a hosted principal`)
	c.Assert(factory, qt.IsNil)
}

// What the API server answers is what the hint says, and a review the API
// server did not complete is unknown, never allowed.
func TestCanTurnsReviewsIntoHints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer func(*authorizationv1.SelfSubjectAccessReview) error
		want   kube.Answer
	}{
		{
			name:   "allowed",
			answer: func(review *authorizationv1.SelfSubjectAccessReview) error { review.Status.Allowed = true; return nil },
			want:   kube.Allowed,
		},
		{
			name:   "denied",
			answer: func(*authorizationv1.SelfSubjectAccessReview) error { return nil },
			want:   kube.Denied,
		},
		{
			name: "allowed beside an evaluation error",
			answer: func(review *authorizationv1.SelfSubjectAccessReview) error {
				review.Status.Allowed = true
				review.Status.EvaluationError = "webhook authorizer timed out"
				return nil
			},
			want: kube.Unknown,
		},
		{
			name:   "the review failed",
			answer: func(*authorizationv1.SelfSubjectAccessReview) error { return errors.New("connection refused") },
			want:   kube.Unknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			reviewer := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, object client.Object, _ ...client.CreateOption) error {
					return test.answer(object.(*authorizationv1.SelfSubjectAccessReview))
				},
			}).Build()

			hints := kube.Can(t.Context(), reviewer,
				kube.Check{Namespace: "application", Verb: "get", Group: "operator.ptah.run", Resource: "ptahschemas"},
				kube.Check{Namespace: "application", Verb: "create", Group: "operator.ptah.run", Resource: "ptahschemaapprovals"})

			c.Assert(hints, qt.HasLen, 2)
			c.Assert(hints[0].Answer, qt.Equals, test.want)
			c.Assert(hints[1].Answer, qt.Equals, test.want)
			c.Assert(hints[1].Check.Resource, qt.Equals, "ptahschemaapprovals")
		})
	}
}
