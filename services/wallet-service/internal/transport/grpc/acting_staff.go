package grpc

import (
	"context"
	"regexp"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// staffActor is who a staff money operation is recorded against: the signed-in
// staff member, or, when support-service calls with the internal token, the
// staff member it already authorized (acting_identity_id). Only a service may
// name someone else; a call with the internal token and no one named is
// refused, as before.
func staffActor(ctx context.Context, actingIdentityID, what string) (string, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return "", status.Error(codes.PermissionDenied, what)
	}

	if principal.IdentityID != internalServicePrincipalID {
		if actingIdentityID != "" {
			return "", status.Error(codes.PermissionDenied, "acting_identity_id is only for services")
		}

		return principal.IdentityID, nil
	}

	if !uuidPattern.MatchString(actingIdentityID) {
		return "", status.Error(codes.PermissionDenied, what)
	}

	return actingIdentityID, nil
}
