package grpc

import (
	"context"
	"errors"
	"testing"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ownershipTestResolver struct {
	riders  map[string]string
	err     error
	lookups int
}

func (m *ownershipTestResolver) RiderID(_ context.Context, identityID string) (string, error) {
	m.lookups++

	return m.riders[identityID], m.err
}

func newOwnershipResolver() *ownershipTestResolver {
	return &ownershipTestResolver{
		riders: map[string]string{"id-rider-a": "rider-a", "id-rider-b": "rider-b"},
	}
}

func callOwnershipAs(t *testing.T, identity string, resolver CallerResolver, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(resolver)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identity,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: pricingRPCPrefix + method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

func estimateFor(riderID string) *pricingv1.EstimateFareRequest {
	return &pricingv1.EstimateFareRequest{RiderId: riderID}
}

func TestRiderEstimatesOnlyForThemselves(t *testing.T) {
	resolver := newOwnershipResolver()

	if code := callOwnershipAs(t, "id-rider-a", resolver, "EstimateFare", estimateFor("rider-a")); code != codes.OK {
		t.Errorf("a rider estimating for themselves: expected OK, got %v", code)
	}

	if code := callOwnershipAs(t, "id-rider-b", resolver, "EstimateFare", estimateFor("rider-a")); code != codes.PermissionDenied {
		t.Errorf("a rider estimating for someone else: expected PermissionDenied, got %v", code)
	}
}

func TestAnIdentityWithoutARiderProfileIsDenied(t *testing.T) {
	// A driver's token, or a token whose rider profile was never created.
	for _, identity := range []string{"id-driver-a", "id-nobody"} {
		if code := callOwnershipAs(t, identity, newOwnershipResolver(), "EstimateFare", estimateFor("rider-a")); code != codes.PermissionDenied {
			t.Errorf("%s: expected PermissionDenied, got %v", identity, code)
		}
	}
}

func TestAnEmptyRiderIDIsDeniedWithoutALookup(t *testing.T) {
	resolver := newOwnershipResolver()

	if code := callOwnershipAs(t, "id-rider-a", resolver, "EstimateFare", estimateFor("")); code != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", code)
	}

	if resolver.lookups != 0 {
		t.Errorf("an empty rider id caused %d profile lookup(s), expected none", resolver.lookups)
	}
}

func TestOwnershipIsUnavailableNotAllowedWhenTheLookupFails(t *testing.T) {
	resolver := newOwnershipResolver()
	resolver.err = errors.New("rider-service down")

	if code := callOwnershipAs(t, "id-rider-a", resolver, "EstimateFare", estimateFor("rider-a")); code != codes.Unavailable {
		t.Errorf("expected Unavailable, got %v", code)
	}
}

func TestNoResolverMeansNoAccess(t *testing.T) {
	if code := callOwnershipAs(t, "id-rider-a", nil, "EstimateFare", estimateFor("rider-a")); code != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", code)
	}
}

func TestWrongRequestTypeIsDenied(t *testing.T) {
	wrong := &pricingv1.GetCouponRequest{Code: "rider-a"}

	if code := callOwnershipAs(t, "id-rider-a", newOwnershipResolver(), "EstimateFare", wrong); code != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", code)
	}
}

func TestEveryOtherRPCIsInternalOnly(t *testing.T) {
	for _, method := range []string{"CalculateFare", "CreateCoupon", "GetCoupon"} {
		for _, identity := range []string{"id-rider-a", "id-driver-a", "id-nobody"} {
			if code := callOwnershipAs(t, identity, newOwnershipResolver(), method, nil); code != codes.PermissionDenied {
				t.Errorf("%s as %s: expected PermissionDenied, got %v", method, identity, code)
			}
		}

		if code := callOwnershipAs(t, internalServicePrincipalID, newOwnershipResolver(), method, nil); code != codes.OK {
			t.Errorf("%s as the internal caller: expected OK, got %v", method, code)
		}
	}
}

func TestInternalCallerEstimatesForAnyRiderWithoutALookup(t *testing.T) {
	resolver := newOwnershipResolver()

	if code := callOwnershipAs(t, internalServicePrincipalID, resolver, "EstimateFare", estimateFor("rider-b")); code != codes.OK {
		t.Errorf("expected OK, got %v", code)
	}

	if resolver.lookups != 0 {
		t.Errorf("the internal caller caused %d profile lookup(s), expected none", resolver.lookups)
	}
}
