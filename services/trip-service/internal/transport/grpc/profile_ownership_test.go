package grpc

import (
	"errors"
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
)

type profileRequest struct {
	name string
	req  func(riderID, driverID string) any
}

// The two RPCs share one check; every case runs against both.
var profileRequests = []profileRequest{
	{"GetActiveTrip", func(r, d string) any { return &tripv1.GetActiveTripRequest{RiderId: r, DriverId: d} }},
	{"ListTrips", func(r, d string) any { return &tripv1.ListTripsRequest{RiderId: r, DriverId: d} }},
}

func callProfileRPCAs(t *testing.T, identity string, pr profileRequest, riderID, driverID string) codes.Code {
	t.Helper()

	return callOwnershipAs(t, identity, ownershipProfiles, ownershipTrips, pr.name, pr.req(riderID, driverID))
}

func TestARiderOrDriverReachesOnlyTheirOwnTrips(t *testing.T) {
	for _, pr := range profileRequests {
		allowed := []struct {
			who      string
			identity string
			rider    string
			driver   string
		}{
			{"a rider asking for their own trips", "id-rider-a", "rider-a", ""},
			{"a driver asking for their own trips", "id-driver-a", "", "driver-a"},
			{"someone with both profiles, as a rider", "id-both", "rider-both", ""},
			{"someone with both profiles, as a driver", "id-both", "", "driver-both"},
		}

		for _, tc := range allowed {
			if code := callProfileRPCAs(t, tc.identity, pr, tc.rider, tc.driver); code != codes.OK {
				t.Errorf("%s / %s: expected OK, got %v", pr.name, tc.who, code)
			}
		}

		denied := []struct {
			who      string
			identity string
			rider    string
			driver   string
		}{
			{"another rider's trips", "id-rider-b", "rider-a", ""},
			{"another driver's trips", "id-driver-b", "", "driver-a"},
			{"a driver asking with a rider's id", "id-driver-a", "rider-a", ""},
			{"a rider asking with a driver's id", "id-rider-a", "", "driver-a"},
			{"a rider asking as a driver", "id-rider-a", "", "driver-both"},
			{"someone with no profile", "id-nobody", "rider-a", ""},
			{"someone with both profiles asking for another rider", "id-both", "rider-a", ""},
			{"a profile that does not exist", "id-rider-a", "no-such-rider", ""},
			{"naming both of their own profiles", "id-both", "rider-both", "driver-both"},
			{"naming both, one of them someone else's", "id-both", "rider-both", "driver-a"},
			{"naming neither", "id-rider-a", "", ""},
		}

		for _, tc := range denied {
			if code := callProfileRPCAs(t, tc.identity, pr, tc.rider, tc.driver); code != codes.PermissionDenied {
				t.Errorf("%s / %s: expected PermissionDenied, got %v", pr.name, tc.who, code)
			}
		}
	}
}

func TestTheInternalServiceMayAskForAnyonesTrips(t *testing.T) {
	for _, pr := range profileRequests {
		if code := callProfileRPCAs(t, internalServicePrincipalID, pr, "rider-a", ""); code != codes.OK {
			t.Errorf("%s: expected OK for the internal service, got %v", pr.name, code)
		}
	}
}

func TestAProfileOwnershipCheckNeedsAProfileRequest(t *testing.T) {
	// A request type that carries no profile ids can never pass.
	wrong := &tripv1.GetTripRequest{TripId: "trip-1"}

	for _, pr := range profileRequests {
		if code := callOwnershipAs(t, "id-rider-a", ownershipProfiles, ownershipTrips, pr.name, wrong); code != codes.PermissionDenied {
			t.Errorf("%s: expected PermissionDenied, got %v", pr.name, code)
		}
	}
}

func TestWhenOwnershipCannotBeVerifiedTheAnswerIsUnavailable(t *testing.T) {
	broken := ownershipTestResolver{err: errors.New("rider-service down")}

	for _, pr := range profileRequests {
		if code := callOwnershipAs(t, "id-rider-a", broken, ownershipTrips, pr.name, pr.req("rider-a", "")); code != codes.Unavailable {
			t.Errorf("%s: expected Unavailable, got %v", pr.name, code)
		}
	}
}
