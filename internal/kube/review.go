package kube

import (
	"context"
	"fmt"
	"sync"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Identity is who the API server says a client is.
type Identity struct {
	Username string
	UID      string
	Groups   []string
}

// WhoAmI asks the API server who the client acts as.
func WhoAmI(ctx context.Context, c client.Client) (Identity, error) {
	review := &authenticationv1.SelfSubjectReview{}
	if err := c.Create(ctx, review); err != nil {
		return Identity{}, fmt.Errorf("kube: ask who this identity is: %w", err)
	}
	user := review.Status.UserInfo
	return Identity{Username: user.Username, UID: user.UID, Groups: user.Groups}, nil
}

// Answer is a capability hint. Unknown is shown as unavailable, never as
// allowed: the real request decides, and a hint that could not be computed
// promises nothing.
type Answer string

// Answers.
const (
	Allowed Answer = "allowed"
	Denied  Answer = "denied"
	Unknown Answer = "unknown"
)

// Check is one question for the API server: may this identity do Verb on
// Resource (and Subresource) in Namespace?
type Check struct {
	Namespace   string
	Verb        string
	Group       string
	Resource    string
	Subresource string
	Name        string
}

// Hint is a check and its answer.
type Hint struct {
	Check  Check
	Answer Answer
	// Reason is what the API server said, or why there is no answer.
	Reason string
}

// maxParallelReviews bounds the reviews one Can call keeps in flight.
const maxParallelReviews = 8

// Can asks the API server each check with SelfSubjectAccessReview and
// returns the hints in the order of the checks.
func Can(ctx context.Context, c client.Client, checks ...Check) []Hint {
	hints := make([]Hint, len(checks))
	slots := make(chan struct{}, maxParallelReviews)
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			hints[i] = can(ctx, c, check)
		})
	}
	wg.Wait()
	return hints
}

func can(ctx context.Context, c client.Client, check Check) Hint {
	review := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace:   check.Namespace,
				Verb:        check.Verb,
				Group:       check.Group,
				Resource:    check.Resource,
				Subresource: check.Subresource,
				Name:        check.Name,
			},
		},
	}
	if err := c.Create(ctx, review); err != nil {
		return Hint{Check: check, Answer: Unknown, Reason: err.Error()}
	}
	status := review.Status
	switch {
	case status.EvaluationError != "":
		// An authorizer failed part of the evaluation; an allowed or denied
		// verdict next to it is not a verdict to show.
		return Hint{Check: check, Answer: Unknown, Reason: status.EvaluationError}
	case status.Allowed:
		return Hint{Check: check, Answer: Allowed, Reason: status.Reason}
	default:
		return Hint{Check: check, Answer: Denied, Reason: status.Reason}
	}
}
