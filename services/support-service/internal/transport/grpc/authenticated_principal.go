package grpc

import "context"

type authenticatedPrincipal struct {
	IdentityID string
	SessionID  string
	TenantHint string
}

type authenticatedPrincipalContextKey struct{}

func contextWithAuthenticatedPrincipal(
	ctx context.Context,
	principal authenticatedPrincipal,
) context.Context {
	return context.WithValue(
		ctx,
		authenticatedPrincipalContextKey{},
		principal,
	)
}

func authenticatedPrincipalFromContext(
	ctx context.Context,
) (authenticatedPrincipal, bool) {
	principal, ok := ctx.Value(
		authenticatedPrincipalContextKey{},
	).(authenticatedPrincipal)

	if !ok {
		return authenticatedPrincipal{}, false
	}

	if principal.IdentityID == "" ||
		principal.SessionID == "" {
		return authenticatedPrincipal{}, false
	}

	return principal, true
}
