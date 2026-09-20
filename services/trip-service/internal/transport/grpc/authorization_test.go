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
	testUnwiredMethod  = "/test.Service/OwnerWithoutCheck"
	testPublicMethod   = "/test.Service/Public"
	testUnknownMethod  = "/test.Service/Unknown"
)

type emptyOwnerResolver struct{}

func (emptyOwnerResolver) RiderID(context.Context, string) (string, error)  { return "", nil }
func (emptyOwnerResolver) DriverID(context.Context, string) (string, error) { return "", nil }

func runAuthorizationCase(
	t *testing.T,
	principal *authenticatedPrincipal,
	method string,
	check ownerCheck,
	resolver CallerResolver,
) (bool, codes.Code) {
	t.Helper()

	checks := map[string]ownerCheck{}
	if check != nil {
		checks[testOwnerMethod] = check
	}

	interceptor := newAuthorizationInterceptor(map[string]accessLevel{
		testInternalMethod: accessInternal,
		testOwnerMethod:    accessOwner,
		testUnwiredMethod:  accessOwner,
		testPublicMethod:   accessAuthenticated,
	}, checks, resolver, nil)

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

func checkAllows(context.Context, caller, any) (bool, error) { return true, nil }
func checkDenies(context.Context, caller, any) (bool, error) { return false, nil }

func TestAuthorizationInternalCallerReachesEveryMethod(t *testing.T) {
	internal := &authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID}

	for _, method := range []string{testInternalMethod, testOwnerMethod, testUnwiredMethod, testPublicMethod, testUnknownMethod} {
		if called, code := runAuthorizationCase(t, internal, method, nil, nil); !called || code != codes.OK {
			t.Fatalf("%s: internal caller must pass, got called=%v code=%v", method, called, code)
		}
	}
}

func TestAuthorizationEndUserReachesAuthenticatedMethods(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	if called, code := runAuthorizationCase(t, user, testPublicMethod, nil, nil); !called || code != codes.OK {
		t.Fatalf("got called=%v code=%v", called, code)
	}
}

func TestAuthorizationEndUserIsDeniedInternalUnknownAndUnwiredMethods(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	for _, method := range []string{testInternalMethod, testUnknownMethod, testUnwiredMethod} {
		if called, code := runAuthorizationCase(t, user, method, checkAllows, emptyOwnerResolver{}); called || code != codes.PermissionDenied {
			t.Fatalf("%s: got called=%v code=%v", method, called, code)
		}
	}
}

func TestAuthorizationOwnerMethodFollowsTheOwnerCheck(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	if called, code := runAuthorizationCase(t, user, testOwnerMethod, checkAllows, emptyOwnerResolver{}); !called || code != codes.OK {
		t.Fatalf("an owner must pass, got called=%v code=%v", called, code)
	}

	if called, code := runAuthorizationCase(t, user, testOwnerMethod, checkDenies, emptyOwnerResolver{}); called || code != codes.PermissionDenied {
		t.Fatalf("a non-owner must be denied, got called=%v code=%v", called, code)
	}
}

func TestAuthorizationOwnerMethodFailsClosedWithoutAResolver(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

	if called, code := runAuthorizationCase(t, user, testOwnerMethod, checkAllows, nil); called || code != codes.PermissionDenied {
		t.Fatalf("got called=%v code=%v", called, code)
	}
}

func TestAuthorizationOwnershipLookupFailureIsUnavailableNotAllowed(t *testing.T) {
	user := &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}
	broken := func(context.Context, caller, any) (bool, error) { return true, errors.New("rider-service down") }

	if called, code := runAuthorizationCase(t, user, testOwnerMethod, broken, emptyOwnerResolver{}); called || code != codes.Unavailable {
		t.Fatalf("got called=%v code=%v", called, code)
	}
}

func TestAuthorizationRequiresAnAuthenticatedPrincipal(t *testing.T) {
	if called, code := runAuthorizationCase(t, nil, testPublicMethod, nil, nil); called || code != codes.Unauthenticated {
		t.Fatalf("got called=%v code=%v", called, code)
	}
}

func TestAuthorizationExemptMethodsNeedNoPrincipal(t *testing.T) {
	if called, code := runAuthorizationCase(t, nil, healthv1.Health_Check_FullMethodName, nil, nil); !called || code != codes.OK {
		t.Fatalf("got called=%v code=%v", called, code)
	}
}
