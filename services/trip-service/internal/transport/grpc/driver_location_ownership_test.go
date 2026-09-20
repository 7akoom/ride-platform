package grpc

import (
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
)

func callDriverLocationAs(t *testing.T, identity, tripID string) codes.Code {
	t.Helper()

	return callOwnershipAs(t, identity, ownershipProfiles, ownershipTrips, "GetDriverLocation", &tripv1.GetDriverLocationRequest{TripId: tripID})
}

func TestOnlyTheRiderOfATripSeesItsDriverLocation(t *testing.T) {
	if code := callDriverLocationAs(t, "id-rider-a", "trip-1"); code != codes.OK {
		t.Errorf("the rider of the trip: expected OK, got %v", code)
	}

	for name, identity := range map[string]string{
		"the driver of the trip":  "id-driver-a",
		"another rider":           "id-rider-b",
		"another driver":          "id-driver-b",
		"someone with no profile": "id-nobody",
		"someone who is both":     "id-both",
	} {
		if code := callDriverLocationAs(t, identity, "trip-1"); code != codes.PermissionDenied {
			t.Errorf("%s: expected PermissionDenied, got %v", name, code)
		}
	}
}

func TestATripThatDoesNotExistLooksLikeSomeoneElsesTrip(t *testing.T) {
	if code := callDriverLocationAs(t, "id-rider-a", "no-such-trip"); code != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", code)
	}
}

func TestAnEmptyTripIDIsDenied(t *testing.T) {
	if code := callDriverLocationAs(t, "id-rider-a", ""); code != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", code)
	}
}

func TestTheRiderOfATripWithoutADriverPassesAuthorization(t *testing.T) {
	// Whether there is anything to show is decided after authorization.
	if code := callDriverLocationAs(t, "id-rider-a", "trip-open"); code != codes.OK {
		t.Errorf("expected OK, got %v", code)
	}
}

func TestTheRequestTypeMustCarryATripID(t *testing.T) {
	wrong := &tripv1.RequestTripRequest{RiderId: "rider-a"}

	if code := callOwnershipAs(t, "id-rider-a", ownershipProfiles, ownershipTrips, "GetDriverLocation", wrong); code != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", code)
	}
}
