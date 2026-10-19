package api

import (
	"context"

	"connectrpc.com/connect/v2"
)

// ReadOnly refuses every call that may change something, which is every
// procedure not declared free of side effects, except the ones allowed, such
// as signing in and out. It does not make the identity's credential weaker
// anywhere but here.
func ReadOnly(allowed ...string) connect.ServerInterceptor {
	open := make(map[string]bool, len(allowed))
	for _, procedure := range allowed {
		open[procedure] = true
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if spec.IdempotencyLevel != connect.IdempotencyNoSideEffects && !open[spec.Procedure] {
				return connect.NewError(connect.CodePermissionDenied, "this console was started with --read-only")
			}
			return next(ctx, spec, stream)
		}
	}
}
