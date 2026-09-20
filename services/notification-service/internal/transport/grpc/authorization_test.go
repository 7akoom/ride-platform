package grpc

import (
	"context"
	"errors"
	"testing"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	testInternalMethod = "/test.Service/Internal"
	testOwnerMethod    = "/test.Service/Owner"
	testPublicMethod   = "/test.Service/Public"
	testUnknownMethod  = "/test.Service/Unknown"
)

func runAuthorizationWith(
	t *testing.T,
	principal *authenticatedPrincipal,
	method string,
	checks map[string]ownerCheck,
) (bool, codes.Code) {
	t.Helper()

	interceptor := newAuthorizationInterceptor(map[string]accessLevel{
		testInternalMethod: accessInternal,
		testOwnerMethod:    accessOwner,
		testPublicMethod:   accessAuthenticated,
	}, checks, nil, nil)

	ctx := context.Background()
	if principal != nil {
		ctx = contextWithAuthenticatedPrincipal(ctx, *principal)
	}

	called := false

	_, err := interceptor(ctx, nil, &googlegrpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
		called = true

		return nil, nil
	})

	return called, status.Code(err)
}

func runAuthorization(t *testing.T, principal *authenticatedPrincipal, method string) (bool, codes.Code) {
	t.Helper()

	return runAuthorizationWith(t, principal, method, nil)
}

func TestAuthorizationInternalCallerReachesEveryMethod(t *testing.T) {
	internal := &authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID}

	for _, method := range []string{testInternalMethod, testOwnerMethod, testPublicMethod, testUnknownMethod} {
		if called, code := runAuthorization(t, internal, method); !called || code != codes.OK {
			t.Fatalf("%s: internal caller must pass, got called=%v code=%v", method, called, code)
		}
	}
}

func TestAuthorizationEndUserIsLimitedToAuthenticatedMethods(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	if called, code := runAuthorization(t, user, testPublicMethod); !called || code != codes.OK {
		t.Fatalf("an end user must reach an authenticated method, got called=%v code=%v", called, code)
	}

	// An owner method with no registered owner check stays closed.
	for _, method := range []string{testInternalMethod, testOwnerMethod, testUnknownMethod} {
		if called, code := runAuthorization(t, user, method); called || code != codes.PermissionDenied {
			t.Fatalf("%s: an end user must be denied, got called=%v code=%v", method, called, code)
		}
	}
}

func TestAuthorizationOwnerMethodFollowsItsOwnerCheck(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	check := func(allowed bool, err error) map[string]ownerCheck {
		return map[string]ownerCheck{
			testOwnerMethod: func(context.Context, caller, any) (bool, error) { return allowed, err },
		}
	}

	if called, code := runAuthorizationWith(t, user, testOwnerMethod, check(true, nil)); !called || code != codes.OK {
		t.Fatalf("an allowing check must let the call through, got called=%v code=%v", called, code)
	}

	if called, code := runAuthorizationWith(t, user, testOwnerMethod, check(false, nil)); called || code != codes.PermissionDenied {
		t.Fatalf("a refusing check must deny, got called=%v code=%v", called, code)
	}

	if called, code := runAuthorizationWith(t, user, testOwnerMethod, check(true, errors.New("lookup failed"))); called || code != codes.Unavailable {
		t.Fatalf("a failing check must be Unavailable and never allow, got called=%v code=%v", called, code)
	}
}

func TestAuthorizationOwnerCheckSeesTheCallersIdentity(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	checks := map[string]ownerCheck{
		testOwnerMethod: func(_ context.Context, c caller, _ any) (bool, error) {
			return c.identityID == "user-1", nil
		},
	}

	if called, code := runAuthorizationWith(t, user, testOwnerMethod, checks); !called || code != codes.OK {
		t.Fatalf("the check must receive the token's identity, got called=%v code=%v", called, code)
	}
}

func TestAuthorizationRequiresAnAuthenticatedPrincipal(t *testing.T) {
	if called, code := runAuthorization(t, nil, testPublicMethod); called || code != codes.Unauthenticated {
		t.Fatalf("no principal must be unauthenticated, got called=%v code=%v", called, code)
	}
}

func TestAuthorizationExemptMethodsNeedNoPrincipal(t *testing.T) {
	if called, code := runAuthorization(t, nil, healthv1.Health_Check_FullMethodName); !called || code != codes.OK {
		t.Fatalf("the health check must be reachable, got called=%v code=%v", called, code)
	}
}
