package grpc

import (
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
)

func TestPickupPhotoIsForTheRiderAndTheDriverOfTheTrip(t *testing.T) {
	request := &tripv1.GetPickupPhotoRequest{TripId: "trip-1"}

	for identity, want := range map[string]codes.Code{
		"id-rider-a":  codes.OK,
		"id-driver-a": codes.OK,
		"id-rider-b":  codes.PermissionDenied,
		"id-driver-b": codes.PermissionDenied,
	} {
		if got := callOwnershipAs(t, identity, ownershipProfiles, ownershipTrips, "GetPickupPhoto", request); got != want {
			t.Errorf("%s: got %v, want %v", identity, got, want)
		}
	}
}

func TestRecentDestinationsAreTheRidersOwn(t *testing.T) {
	request := &tripv1.ListRecentDestinationsRequest{RiderId: "rider-a"}

	for identity, want := range map[string]codes.Code{
		"id-rider-a":  codes.OK,
		"id-rider-b":  codes.PermissionDenied,
		"id-driver-a": codes.PermissionDenied,
	} {
		if got := callOwnershipAs(t, identity, ownershipProfiles, ownershipTrips, "ListRecentDestinations", request); got != want {
			t.Errorf("%s: got %v, want %v", identity, got, want)
		}
	}
}
