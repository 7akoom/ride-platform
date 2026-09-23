package grpc

import (
	"context"
	"errors"
	"testing"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ownershipDriverA       = "11111111-1111-4111-8111-111111111111"
	ownershipDriverB       = "22222222-2222-4222-8222-222222222222"
	ownershipMissingDriver = "33333333-3333-4333-8333-333333333333"
)

type ownershipTestDrivers struct {
	drivers map[string]driver.Driver
	err     error
	lookups int
}

func (m *ownershipTestDrivers) GetDriver(_ context.Context, driverID string) (driver.Driver, error) {
	m.lookups++

	if m.err != nil {
		return driver.Driver{}, m.err
	}

	found, ok := m.drivers[driverID]
	if !ok {
		return driver.Driver{}, driver.ErrDriverNotFound
	}

	return found, nil
}

func newOwnershipDrivers() *ownershipTestDrivers {
	return &ownershipTestDrivers{drivers: map[string]driver.Driver{
		ownershipDriverA: {ID: ownershipDriverA, IdentityID: "identity-a"},
		ownershipDriverB: {ID: ownershipDriverB, IdentityID: "identity-b"},
	}}
}

func callOwnershipAs(t *testing.T, identity string, drivers DriverReader, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(drivers, nil)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identity,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: driverRPCPrefix + method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

// ownershipRequests builds one request per RPC, all aimed at the given driver
// profile and identity.
func ownershipRequests(driverID, identityID string) map[string]any {
	return map[string]any{
		"CreateDriver":        &driverv1.CreateDriverRequest{IdentityId: identityID, DisplayName: "x"},
		"GetDriverByIdentity": &driverv1.GetDriverByIdentityRequest{IdentityId: identityID},
		"GetDriver":           &driverv1.GetDriverRequest{DriverId: driverID},
		"UpdateDriverProfile": &driverv1.UpdateDriverProfileRequest{DriverId: driverID, DisplayName: "x"},
		"UpdateAvailability": &driverv1.UpdateAvailabilityRequest{
			DriverId:           driverID,
			AvailabilityStatus: driverv1.AvailabilityStatus_AVAILABILITY_STATUS_AVAILABLE,
		},
	}
}

func expectOwnershipCodes(t *testing.T, drivers DriverReader, identity string, want codes.Code, requests map[string]any) {
	t.Helper()

	for method, request := range requests {
		if code := callOwnershipAs(t, identity, drivers, method, request); code != want {
			t.Errorf("%s as %s: expected %v, got %v", method, identity, want, code)
		}
	}
}

func TestDriverReachesTheirOwnProfile(t *testing.T) {
	expectOwnershipCodes(t, newOwnershipDrivers(), "identity-a", codes.OK, ownershipRequests(ownershipDriverA, "identity-a"))
}

func TestDriverCannotReachSomeoneElsesProfile(t *testing.T) {
	// Driver B aims every RPC at driver A's profile and identity.
	expectOwnershipCodes(t, newOwnershipDrivers(), "identity-b", codes.PermissionDenied, ownershipRequests(ownershipDriverA, "identity-a"))
}

func TestDriverCannotCreateOrLookUpAnotherIdentity(t *testing.T) {
	drivers := newOwnershipDrivers()

	for _, method := range []string{"CreateDriver", "GetDriverByIdentity"} {
		request := ownershipRequests(ownershipDriverB, "identity-b")[method]

		if code := callOwnershipAs(t, "identity-a", drivers, method, request); code != codes.PermissionDenied {
			t.Errorf("%s naming another identity: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestIdentityWithoutAProfileIsDeniedEveryProfileLookup(t *testing.T) {
	drivers := newOwnershipDrivers()

	for _, method := range []string{"GetDriver", "UpdateDriverProfile", "UpdateAvailability"} {
		request := ownershipRequests(ownershipDriverA, "identity-a")[method]

		if code := callOwnershipAs(t, "identity-nobody", drivers, method, request); code != codes.PermissionDenied {
			t.Errorf("%s by an identity with no profile: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestMissingProfilesLookLikeSomeoneElsesProfiles(t *testing.T) {
	drivers := newOwnershipDrivers()

	for _, method := range []string{"GetDriver", "UpdateDriverProfile", "UpdateAvailability"} {
		request := ownershipRequests(ownershipMissingDriver, "identity-a")[method]

		if code := callOwnershipAs(t, "identity-a", drivers, method, request); code != codes.PermissionDenied {
			t.Errorf("%s on a missing profile: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestMalformedIDsAreDeniedWithoutALookup(t *testing.T) {
	for _, badID := range []string{"", "not-a-uuid", ownershipDriverA + "x", "'; drop table drivers;--"} {
		drivers := newOwnershipDrivers()

		for _, method := range []string{"GetDriver", "UpdateDriverProfile", "UpdateAvailability"} {
			request := ownershipRequests(badID, "identity-a")[method]

			if code := callOwnershipAs(t, "identity-a", drivers, method, request); code != codes.PermissionDenied {
				t.Errorf("%s with id %q: expected PermissionDenied, got %v", method, badID, code)
			}
		}

		if drivers.lookups != 0 {
			t.Errorf("id %q reached the repository %d time(s); malformed ids must be denied first", badID, drivers.lookups)
		}
	}
}

func TestEmptyIdentityInARequestIsDenied(t *testing.T) {
	drivers := newOwnershipDrivers()

	for _, method := range []string{"CreateDriver", "GetDriverByIdentity"} {
		request := ownershipRequests(ownershipDriverA, "")[method]

		if code := callOwnershipAs(t, "identity-a", drivers, method, request); code != codes.PermissionDenied {
			t.Errorf("%s with no identity: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestIdentityChecksNeedNoLookup(t *testing.T) {
	drivers := newOwnershipDrivers()
	requests := ownershipRequests(ownershipDriverA, "identity-a")

	for _, method := range []string{"CreateDriver", "GetDriverByIdentity"} {
		if code := callOwnershipAs(t, "identity-a", drivers, method, requests[method]); code != codes.OK {
			t.Errorf("%s: expected OK, got %v", method, code)
		}
	}

	if drivers.lookups != 0 {
		t.Errorf("identity-only checks made %d repository lookup(s), expected none", drivers.lookups)
	}
}

func TestOwnershipIsUnavailableNotAllowedWhenTheLookupFails(t *testing.T) {
	drivers := newOwnershipDrivers()
	drivers.err = errors.New("database down")

	requests := ownershipRequests(ownershipDriverA, "identity-a")

	for _, method := range []string{"GetDriver", "UpdateDriverProfile", "UpdateAvailability"} {
		if code := callOwnershipAs(t, "identity-a", drivers, method, requests[method]); code != codes.Unavailable {
			t.Errorf("%s with a failing lookup: expected Unavailable, got %v", method, code)
		}
	}

	// The identity-only checks do not depend on the database.
	for _, method := range []string{"CreateDriver", "GetDriverByIdentity"} {
		if code := callOwnershipAs(t, "identity-a", drivers, method, requests[method]); code != codes.OK {
			t.Errorf("%s does not need the database: expected OK, got %v", method, code)
		}
	}
}

func TestNoLookupReaderMeansNoAccessToProfileIDs(t *testing.T) {
	requests := ownershipRequests(ownershipDriverA, "identity-a")

	for _, method := range []string{"GetDriver", "UpdateDriverProfile", "UpdateAvailability"} {
		if code := callOwnershipAs(t, "identity-a", nil, method, requests[method]); code != codes.PermissionDenied {
			t.Errorf("%s without a reader: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestWrongRequestTypeIsDenied(t *testing.T) {
	drivers := newOwnershipDrivers()

	wrong := &driverv1.GetDriverByIdentityRequest{IdentityId: "identity-a"}

	for _, method := range []string{"GetDriver", "UpdateDriverProfile", "UpdateAvailability", "CreateDriver"} {
		if code := callOwnershipAs(t, "identity-a", drivers, method, wrong); code != codes.PermissionDenied {
			t.Errorf("%s with the wrong request type: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestInternalCallerReachesEveryProfileWithoutALookup(t *testing.T) {
	drivers := newOwnershipDrivers()

	expectOwnershipCodes(t, drivers, internalServicePrincipalID, codes.OK, ownershipRequests(ownershipDriverB, "identity-b"))

	if drivers.lookups != 0 {
		t.Errorf("the internal caller made %d repository lookup(s), expected none", drivers.lookups)
	}
}
