package grpc

import (
	"context"
	"errors"
	"testing"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ownershipRiderA       = "11111111-1111-4111-8111-111111111111"
	ownershipRiderB       = "22222222-2222-4222-8222-222222222222"
	ownershipMissingRider = "33333333-3333-4333-8333-333333333333"
)

type ownershipTestRiders struct {
	riders  map[string]rider.Rider
	err     error
	lookups int
}

func (m *ownershipTestRiders) GetRider(_ context.Context, riderID string) (rider.Rider, error) {
	m.lookups++

	if m.err != nil {
		return rider.Rider{}, m.err
	}

	found, ok := m.riders[riderID]
	if !ok {
		return rider.Rider{}, rider.ErrRiderNotFound
	}

	return found, nil
}

func newOwnershipRiders() *ownershipTestRiders {
	return &ownershipTestRiders{riders: map[string]rider.Rider{
		ownershipRiderA: {ID: ownershipRiderA, IdentityID: "identity-a"},
		ownershipRiderB: {ID: ownershipRiderB, IdentityID: "identity-b"},
	}}
}

func callOwnershipAs(t *testing.T, identity string, riders RiderReader, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(riders)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identity,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: riderRPCPrefix + method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

// ownershipRequests builds one request per RPC, all aimed at the given rider
// profile and identity.
func ownershipRequests(riderID, identityID string) map[string]any {
	return map[string]any{
		"CreateRider":        &riderv1.CreateRiderRequest{IdentityId: identityID, DisplayName: "x"},
		"GetRiderByIdentity": &riderv1.GetRiderByIdentityRequest{IdentityId: identityID},
		"GetRider":           &riderv1.GetRiderRequest{RiderId: riderID},
		"UpdateRiderProfile": &riderv1.UpdateRiderProfileRequest{RiderId: riderID, DisplayName: "x"},
	}
}

func expectOwnershipCodes(t *testing.T, riders RiderReader, identity string, want codes.Code, requests map[string]any) {
	t.Helper()

	for method, request := range requests {
		if code := callOwnershipAs(t, identity, riders, method, request); code != want {
			t.Errorf("%s as %s: expected %v, got %v", method, identity, want, code)
		}
	}
}

func TestRiderReachesTheirOwnProfile(t *testing.T) {
	expectOwnershipCodes(t, newOwnershipRiders(), "identity-a", codes.OK, ownershipRequests(ownershipRiderA, "identity-a"))
}

func TestRiderCannotReachSomeoneElsesProfile(t *testing.T) {
	// Rider B aims every RPC at rider A's profile and identity.
	expectOwnershipCodes(t, newOwnershipRiders(), "identity-b", codes.PermissionDenied, ownershipRequests(ownershipRiderA, "identity-a"))
}

func TestRiderCannotCreateOrLookUpAnotherIdentity(t *testing.T) {
	riders := newOwnershipRiders()

	for _, method := range []string{"CreateRider", "GetRiderByIdentity"} {
		request := ownershipRequests(ownershipRiderB, "identity-b")[method]

		if code := callOwnershipAs(t, "identity-a", riders, method, request); code != codes.PermissionDenied {
			t.Errorf("%s naming another identity: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestIdentityWithoutAProfileIsDeniedEveryProfileLookup(t *testing.T) {
	riders := newOwnershipRiders()

	for _, method := range []string{"GetRider", "UpdateRiderProfile"} {
		request := ownershipRequests(ownershipRiderA, "identity-a")[method]

		if code := callOwnershipAs(t, "identity-nobody", riders, method, request); code != codes.PermissionDenied {
			t.Errorf("%s by an identity with no profile: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestMissingProfilesLookLikeSomeoneElsesProfiles(t *testing.T) {
	riders := newOwnershipRiders()

	for _, method := range []string{"GetRider", "UpdateRiderProfile"} {
		request := ownershipRequests(ownershipMissingRider, "identity-a")[method]

		if code := callOwnershipAs(t, "identity-a", riders, method, request); code != codes.PermissionDenied {
			t.Errorf("%s on a missing profile: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestMalformedIDsAreDeniedWithoutALookup(t *testing.T) {
	for _, badID := range []string{"", "not-a-uuid", ownershipRiderA + "x", "'; drop table riders;--"} {
		riders := newOwnershipRiders()

		for _, method := range []string{"GetRider", "UpdateRiderProfile"} {
			request := ownershipRequests(badID, "identity-a")[method]

			if code := callOwnershipAs(t, "identity-a", riders, method, request); code != codes.PermissionDenied {
				t.Errorf("%s with id %q: expected PermissionDenied, got %v", method, badID, code)
			}
		}

		if riders.lookups != 0 {
			t.Errorf("id %q reached the repository %d time(s); malformed ids must be denied first", badID, riders.lookups)
		}
	}
}

func TestEmptyIdentityInARequestIsDenied(t *testing.T) {
	riders := newOwnershipRiders()

	for _, method := range []string{"CreateRider", "GetRiderByIdentity"} {
		request := ownershipRequests(ownershipRiderA, "")[method]

		if code := callOwnershipAs(t, "identity-a", riders, method, request); code != codes.PermissionDenied {
			t.Errorf("%s with no identity: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestIdentityChecksNeedNoLookup(t *testing.T) {
	riders := newOwnershipRiders()
	requests := ownershipRequests(ownershipRiderA, "identity-a")

	for _, method := range []string{"CreateRider", "GetRiderByIdentity"} {
		if code := callOwnershipAs(t, "identity-a", riders, method, requests[method]); code != codes.OK {
			t.Errorf("%s: expected OK, got %v", method, code)
		}
	}

	if riders.lookups != 0 {
		t.Errorf("identity-only checks made %d repository lookup(s), expected none", riders.lookups)
	}
}

func TestOwnershipIsUnavailableNotAllowedWhenTheLookupFails(t *testing.T) {
	riders := newOwnershipRiders()
	riders.err = errors.New("database down")

	requests := ownershipRequests(ownershipRiderA, "identity-a")

	for _, method := range []string{"GetRider", "UpdateRiderProfile"} {
		if code := callOwnershipAs(t, "identity-a", riders, method, requests[method]); code != codes.Unavailable {
			t.Errorf("%s with a failing lookup: expected Unavailable, got %v", method, code)
		}
	}

	// The identity-only checks do not depend on the database.
	for _, method := range []string{"CreateRider", "GetRiderByIdentity"} {
		if code := callOwnershipAs(t, "identity-a", riders, method, requests[method]); code != codes.OK {
			t.Errorf("%s does not need the database: expected OK, got %v", method, code)
		}
	}
}

func TestNoLookupReaderMeansNoAccessToProfileIDs(t *testing.T) {
	requests := ownershipRequests(ownershipRiderA, "identity-a")

	for _, method := range []string{"GetRider", "UpdateRiderProfile"} {
		if code := callOwnershipAs(t, "identity-a", nil, method, requests[method]); code != codes.PermissionDenied {
			t.Errorf("%s without a reader: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestWrongRequestTypeIsDenied(t *testing.T) {
	riders := newOwnershipRiders()

	wrong := &riderv1.GetRiderByIdentityRequest{IdentityId: "identity-a"}

	for _, method := range []string{"GetRider", "UpdateRiderProfile", "CreateRider"} {
		if code := callOwnershipAs(t, "identity-a", riders, method, wrong); code != codes.PermissionDenied {
			t.Errorf("%s with the wrong request type: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestInternalCallerReachesEveryProfileWithoutALookup(t *testing.T) {
	riders := newOwnershipRiders()

	expectOwnershipCodes(t, riders, internalServicePrincipalID, codes.OK, ownershipRequests(ownershipRiderB, "identity-b"))

	if riders.lookups != 0 {
		t.Errorf("the internal caller made %d repository lookup(s), expected none", riders.lookups)
	}
}
