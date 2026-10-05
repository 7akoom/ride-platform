package grpc

import (
	"context"
	"errors"
	"testing"

	analyticsv1 "github.com/7akoom/ride-platform/gen/go/ride/analytics/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	testInternalMethod = "/test.Service/Internal"
	testStaffMethod    = "/test.Service/Staff"
	testPublicMethod   = "/test.Service/Public"
	testUnknownMethod  = "/test.Service/Unknown"
)

type fakeStaff struct {
	allowed    bool
	err        error
	asked      []string
	targets    []string
	completed  []codes.Code
	identities []string
}

func (f *fakeStaff) Authorize(_ context.Context, identityID, permission, _, targetID string) (bool, string, error) {
	f.asked = append(f.asked, permission)
	f.targets = append(f.targets, targetID)
	f.identities = append(f.identities, identityID)

	return f.allowed, "audit-1", f.err
}

func (f *fakeStaff) Complete(_ context.Context, _ string, code codes.Code) {
	f.completed = append(f.completed, code)
}

func runAuthorization(t *testing.T, principal *authenticatedPrincipal, method string, staff StaffAuthorizer, request any) (bool, codes.Code) {
	t.Helper()

	interceptor := newAuthorizationInterceptor(map[string]accessLevel{
		testInternalMethod: accessInternal,
		testStaffMethod:    accessStaff,
		testPublicMethod:   accessAuthenticated,
	}, map[string]string{testStaffMethod: permissionAnalyticsRead}, staff)

	ctx := context.Background()
	if principal != nil {
		ctx = contextWithAuthenticatedPrincipal(ctx, *principal)
	}

	called := false

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
		called = true

		return nil, nil
	})

	return called, status.Code(err)
}

var user = &authenticatedPrincipal{IdentityID: "user-1", SessionID: "session-1"}

func TestAuthorizationInternalCallerReachesEveryMethod(t *testing.T) {
	internal := &authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID}

	for _, method := range []string{testInternalMethod, testStaffMethod, testPublicMethod, testUnknownMethod} {
		if called, code := runAuthorization(t, internal, method, nil, nil); !called || code != codes.OK {
			t.Fatalf("%s: internal caller must pass, got called=%v code=%v", method, called, code)
		}
	}
}

func TestAuthorizationEndUserIsLimitedToAuthenticatedMethods(t *testing.T) {
	if called, code := runAuthorization(t, user, testPublicMethod, nil, nil); !called || code != codes.OK {
		t.Fatalf("an end user must reach an authenticated method, got called=%v code=%v", called, code)
	}

	for _, method := range []string{testInternalMethod, testUnknownMethod} {
		if called, code := runAuthorization(t, user, method, &fakeStaff{allowed: true}, nil); called || code != codes.PermissionDenied {
			t.Fatalf("%s: an end user must be denied, got called=%v code=%v", method, called, code)
		}
	}
}

func TestAuthorizationReportsNeedTheStaffPermission(t *testing.T) {
	allowed := &fakeStaff{allowed: true}
	request := &analyticsv1.GetTripFunnelRequest{Scope: &analyticsv1.ReportScope{CityId: "city-1"}}

	if called, code := runAuthorization(t, user, testStaffMethod, allowed, request); !called || code != codes.OK {
		t.Fatalf("allowed staff: called=%v code=%v", called, code)
	}

	if len(allowed.asked) != 1 || allowed.asked[0] != "analytics.read" || allowed.targets[0] != "city-1" ||
		allowed.identities[0] != "user-1" || len(allowed.completed) != 1 || allowed.completed[0] != codes.OK {
		t.Fatalf("staff-service was asked %+v", allowed)
	}

	if called, code := runAuthorization(t, user, testStaffMethod, &fakeStaff{}, request); called || code != codes.PermissionDenied {
		t.Fatalf("refused staff: called=%v code=%v", called, code)
	}

	down := &fakeStaff{allowed: true, err: errors.New("down")}
	if called, code := runAuthorization(t, user, testStaffMethod, down, request); called || code != codes.Unavailable {
		t.Fatalf("staff-service down must fail closed: called=%v code=%v", called, code)
	}

	if called, code := runAuthorization(t, user, testStaffMethod, nil, request); called || code != codes.PermissionDenied {
		t.Fatalf("no authorizer must deny: called=%v code=%v", called, code)
	}
}

func TestEveryReportNeedsAnalyticsRead(t *testing.T) {
	for method, level := range methodAccess {
		if level == accessStaff && staffPermissions[method] != permissionAnalyticsRead {
			t.Errorf("%s has permission %q", method, staffPermissions[method])
		}
	}

	for method := range staffPermissions {
		if methodAccess[method] != accessStaff {
			t.Errorf("%s has a permission but is not a staff method", method)
		}
	}
}

func TestAuthorizationRequiresAnAuthenticatedPrincipal(t *testing.T) {
	if called, code := runAuthorization(t, nil, testPublicMethod, nil, nil); called || code != codes.Unauthenticated {
		t.Fatalf("no principal must be unauthenticated, got called=%v code=%v", called, code)
	}
}

func TestAuthorizationExemptMethodsNeedNoPrincipal(t *testing.T) {
	if called, code := runAuthorization(t, nil, healthv1.Health_Check_FullMethodName, nil, nil); !called || code != codes.OK {
		t.Fatalf("the health check must be reachable, got called=%v code=%v", called, code)
	}
}
