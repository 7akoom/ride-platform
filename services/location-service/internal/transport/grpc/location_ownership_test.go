package grpc

import (
	"context"
	"errors"
	"testing"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ownershipTestResolver struct {
	riders  map[string]string
	drivers map[string]string
	err     error
	lookups int
}

func (m *ownershipTestResolver) RiderID(_ context.Context, identityID string) (string, error) {
	m.lookups++

	return m.riders[identityID], m.err
}

func (m *ownershipTestResolver) DriverID(_ context.Context, identityID string) (string, error) {
	m.lookups++

	return m.drivers[identityID], m.err
}

func newOwnershipResolver() *ownershipTestResolver {
	return &ownershipTestResolver{
		riders:  map[string]string{"id-rider-a": "rider-a", "id-rider-b": "rider-b", "id-both": "rider-both"},
		drivers: map[string]string{"id-driver-a": "driver-a", "id-driver-b": "driver-b", "id-both": "driver-both"},
	}
}

func callOwnershipAs(t *testing.T, identity string, resolver CallerResolver, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(resolver, nil)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identity,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: locationRPCPrefix + method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

// locationRequests builds both owner-checked requests for one entity.
func locationRequests(entityType locationv1.EntityType, entityID string) map[string]any {
	return map[string]any{
		"UpdateLocation": &locationv1.UpdateLocationRequest{EntityType: entityType, EntityId: entityID},
		"GetLocation":    &locationv1.GetLocationRequest{EntityType: entityType, EntityId: entityID},
	}
}

func expectLocationCodes(t *testing.T, resolver CallerResolver, identity string, want codes.Code, requests map[string]any) {
	t.Helper()

	for method, request := range requests {
		if code := callOwnershipAs(t, identity, resolver, method, request); code != want {
			t.Errorf("%s as %s: expected %v, got %v", method, identity, want, code)
		}
	}
}

func TestDriverReachesOnlyTheirOwnLocation(t *testing.T) {
	driver := locationv1.EntityType_ENTITY_TYPE_DRIVER

	expectLocationCodes(t, newOwnershipResolver(), "id-driver-a", codes.OK, locationRequests(driver, "driver-a"))
	expectLocationCodes(t, newOwnershipResolver(), "id-driver-b", codes.PermissionDenied, locationRequests(driver, "driver-a"))
}

func TestRiderReachesOnlyTheirOwnLocation(t *testing.T) {
	rider := locationv1.EntityType_ENTITY_TYPE_RIDER

	expectLocationCodes(t, newOwnershipResolver(), "id-rider-a", codes.OK, locationRequests(rider, "rider-a"))
	expectLocationCodes(t, newOwnershipResolver(), "id-rider-b", codes.PermissionDenied, locationRequests(rider, "rider-a"))
}

func TestARiderCannotMoveOrReadADriver(t *testing.T) {
	// This is what stops a rider spoofing or tracking a driver directly:
	// a rider follows their driver through the trip, not through location.
	expectLocationCodes(t, newOwnershipResolver(), "id-rider-a", codes.PermissionDenied,
		locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, "driver-a"))
}

func TestADriverCannotActAsARider(t *testing.T) {
	expectLocationCodes(t, newOwnershipResolver(), "id-driver-a", codes.PermissionDenied,
		locationRequests(locationv1.EntityType_ENTITY_TYPE_RIDER, "driver-a"))
}

func TestSomeoneWhoIsBothIsCheckedPerEntityType(t *testing.T) {
	resolver := newOwnershipResolver()

	expectLocationCodes(t, resolver, "id-both", codes.OK, locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, "driver-both"))
	expectLocationCodes(t, resolver, "id-both", codes.OK, locationRequests(locationv1.EntityType_ENTITY_TYPE_RIDER, "rider-both"))

	// The rider id under the driver type, and the driver id under the rider type.
	expectLocationCodes(t, resolver, "id-both", codes.PermissionDenied, locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, "rider-both"))
	expectLocationCodes(t, resolver, "id-both", codes.PermissionDenied, locationRequests(locationv1.EntityType_ENTITY_TYPE_RIDER, "driver-both"))
}

func TestAnIdentityWithoutAProfileIsDenied(t *testing.T) {
	for _, entityType := range []locationv1.EntityType{
		locationv1.EntityType_ENTITY_TYPE_DRIVER,
		locationv1.EntityType_ENTITY_TYPE_RIDER,
	} {
		expectLocationCodes(t, newOwnershipResolver(), "id-nobody", codes.PermissionDenied, locationRequests(entityType, "driver-a"))
	}
}

func TestUnspecifiedEntityTypeAndEmptyIDsAreDeniedWithoutALookup(t *testing.T) {
	resolver := newOwnershipResolver()

	expectLocationCodes(t, resolver, "id-driver-a", codes.PermissionDenied, locationRequests(locationv1.EntityType_ENTITY_TYPE_UNSPECIFIED, "driver-a"))
	expectLocationCodes(t, resolver, "id-driver-a", codes.PermissionDenied, locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, ""))
	expectLocationCodes(t, resolver, "id-rider-a", codes.PermissionDenied, locationRequests(locationv1.EntityType_ENTITY_TYPE_RIDER, ""))

	if resolver.lookups != 0 {
		t.Errorf("unusable requests caused %d profile lookup(s), expected none", resolver.lookups)
	}
}

func TestOwnershipIsUnavailableNotAllowedWhenTheLookupFails(t *testing.T) {
	resolver := newOwnershipResolver()
	resolver.err = errors.New("driver-service down")

	expectLocationCodes(t, resolver, "id-driver-a", codes.Unavailable, locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, "driver-a"))
	expectLocationCodes(t, resolver, "id-rider-a", codes.Unavailable, locationRequests(locationv1.EntityType_ENTITY_TYPE_RIDER, "rider-a"))
}

func TestNoResolverMeansNoAccess(t *testing.T) {
	expectLocationCodes(t, nil, "id-driver-a", codes.PermissionDenied, locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, "driver-a"))
}

func TestWrongRequestTypeIsDenied(t *testing.T) {
	resolver := newOwnershipResolver()

	wrong := &locationv1.FindNearbyRequest{EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER}

	for _, method := range []string{"UpdateLocation", "GetLocation"} {
		if code := callOwnershipAs(t, "id-driver-a", resolver, method, wrong); code != codes.PermissionDenied {
			t.Errorf("%s with the wrong request type: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestFindNearbyIsInternalOnly(t *testing.T) {
	request := &locationv1.FindNearbyRequest{EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER}

	for _, identity := range []string{"id-driver-a", "id-rider-a", "id-both", "id-nobody"} {
		if code := callOwnershipAs(t, identity, newOwnershipResolver(), "FindNearby", request); code != codes.PermissionDenied {
			t.Errorf("FindNearby as %s: expected PermissionDenied, got %v", identity, code)
		}
	}

	if code := callOwnershipAs(t, internalServicePrincipalID, newOwnershipResolver(), "FindNearby", request); code != codes.OK {
		t.Errorf("FindNearby as the internal caller: expected OK, got %v", code)
	}
}

func TestZonesKeepTheirAccessLevels(t *testing.T) {
	resolver := newOwnershipResolver()

	for _, method := range []string{"GetZone", "ListZones", "CheckServiceZone"} {
		if code := callOwnershipAs(t, "id-rider-a", resolver, method, nil); code != codes.OK {
			t.Errorf("%s as an end user: expected OK, got %v", method, code)
		}
	}

	for _, method := range []string{"CreateZone", "UpdateZone", "SetZoneActive"} {
		if code := callOwnershipAs(t, "id-driver-a", resolver, method, nil); code != codes.PermissionDenied {
			t.Errorf("%s as an end user: expected PermissionDenied, got %v", method, code)
		}

		if code := callOwnershipAs(t, internalServicePrincipalID, resolver, method, nil); code != codes.OK {
			t.Errorf("%s as the internal caller: expected OK, got %v", method, code)
		}
	}
}

func TestInternalCallerReachesEveryLocationWithoutALookup(t *testing.T) {
	resolver := newOwnershipResolver()

	expectLocationCodes(t, resolver, internalServicePrincipalID, codes.OK, locationRequests(locationv1.EntityType_ENTITY_TYPE_DRIVER, "driver-b"))
	expectLocationCodes(t, resolver, internalServicePrincipalID, codes.OK, locationRequests(locationv1.EntityType_ENTITY_TYPE_RIDER, "rider-b"))

	if resolver.lookups != 0 {
		t.Errorf("the internal caller caused %d profile lookup(s), expected none", resolver.lookups)
	}
}
