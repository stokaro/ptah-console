package api

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"connectrpc.com/connect/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/stokaro/ptah-console/internal/auth/session"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/kube"
)

// Scope is which namespaces the console offers and may read.
type Scope struct {
	// Candidates are offered when nothing else names namespaces: the
	// --namespace flags in local mode, the ceiling in hosted mode.
	Candidates []string
	// Ceiling, when set, is the only set of namespaces the console reads,
	// whatever the identity may read elsewhere.
	Ceiling []string
	// MaxPerNamespace bounds the objects of one kind read from one
	// namespace; zero means DefaultMaxPerNamespace.
	MaxPerNamespace int
}

// DefaultMaxPerNamespace bounds a list when the scope does not.
const DefaultMaxPerNamespace = 1000

// listPage is the most objects one list request asks for.
const listPage = 250

func (s Scope) maxPerNamespace() int {
	if s.MaxPerNamespace > 0 {
		return s.MaxPerNamespace
	}
	return DefaultMaxPerNamespace
}

// maxNamespaces bounds one request.
const maxNamespaces = 64

var namespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// check refuses a namespace name that is not one, or one outside the ceiling.
func (s Scope) check(namespaces []string) error {
	if len(namespaces) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, "name at least one namespace")
	}
	if len(namespaces) > maxNamespaces {
		return connect.Errorf(connect.CodeInvalidArgument, "at most %d namespaces per request", maxNamespaces)
	}
	for _, namespace := range namespaces {
		if !namespacePattern.MatchString(namespace) {
			return connect.Errorf(connect.CodeInvalidArgument, "%q is not a namespace name", namespace)
		}
		if len(s.Ceiling) > 0 && !slices.Contains(s.Ceiling, namespace) {
			return connect.Errorf(connect.CodePermissionDenied, "namespace %q is outside what this console serves", namespace)
		}
	}
	return nil
}

// actor is the session's principal and its client.
func actor(ctx context.Context, factory kube.ClientFactory) (*kube.Client, kube.Principal, error) {
	current, ok := session.FromContext(ctx)
	if !ok {
		return nil, kube.Principal{}, connect.NewError(connect.CodeUnauthenticated, "sign in first")
	}
	principal := current.Principal()
	client, err := factory.For(principal)
	if err != nil {
		return nil, principal, connect.NewError(connect.CodeFailedPrecondition, "this session's identity is no longer the console's").WithCause(err)
	}
	return client, principal, nil
}

// readError turns an API error into what the browser is told. The API
// server's own message is public to this identity: it is the answer the same
// request would get from kubectl.
func readError(err error, what string) error {
	var status apierrors.APIStatus
	switch {
	case errors.As(err, &status) && apierrors.IsForbidden(err):
		return connect.NewError(connect.CodePermissionDenied, status.Status().Message).WithCause(err)
	case apierrors.IsNotFound(err):
		return connect.NewError(connect.CodeNotFound, fmt.Sprintf("%s does not exist", what)).WithCause(err)
	case errors.Is(err, context.Canceled):
		// The caller went away, as a page does when it navigates while a
		// read is in flight. The API server did nothing wrong.
		return connect.NewError(connect.CodeCanceled, "the request was cancelled").WithCause(err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, "the API server did not answer in time").WithCause(err)
	default:
		return connect.NewError(connect.CodeUnavailable, fmt.Sprintf("the API server did not return %s", what)).WithCause(err)
	}
}

// namespaceResult says how the read of one namespace went.
func namespaceResult(namespace string, err error) *consolev1.NamespaceResult {
	switch {
	case err == nil:
		return &consolev1.NamespaceResult{Namespace: namespace, Access: consolev1.Access_ACCESS_OK}
	case apierrors.IsForbidden(err):
		return &consolev1.NamespaceResult{Namespace: namespace, Access: consolev1.Access_ACCESS_FORBIDDEN, Reason: err.Error()}
	default:
		return &consolev1.NamespaceResult{Namespace: namespace, Access: consolev1.Access_ACCESS_ERROR, Reason: "the API server did not answer"}
	}
}
