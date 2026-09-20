package grpc

import (
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
)

type offerRequest struct {
	name string
	req  func(driverID string) any
}

// The three driver-facing RPCs share one check; every case runs against all of them.
var offerRequests = []offerRequest{
	{"GetPendingOffer", func(d string) any { return &tripv1.GetPendingOfferRequest{DriverId: d} }},
	{"AcceptOffer", func(d string) any { return &tripv1.AcceptOfferRequest{TripId: "trip-1", DriverId: d} }},
	{"RejectOffer", func(d string) any { return &tripv1.RejectOfferRequest{TripId: "trip-1", DriverId: d} }},
}

func TestADriverAnswersOnlyAsThemselves(t *testing.T) {
	for _, or := range offerRequests {
		allowed := map[string]struct{ identity, driver string }{
			"a driver as themselves":                  {"id-driver-a", "driver-a"},
			"someone with both profiles, as a driver": {"id-both", "driver-both"},
		}

		for who, tc := range allowed {
			if code := callOwnershipAs(t, tc.identity, ownershipProfiles, ownershipTrips, or.name, or.req(tc.driver)); code != codes.OK {
				t.Errorf("%s / %s: expected OK, got %v", or.name, who, code)
			}
		}

		denied := map[string]struct{ identity, driver string }{
			"a driver as another driver":              {"id-driver-a", "driver-b"},
			"a rider naming a driver":                 {"id-rider-a", "driver-a"},
			"a rider naming their own rider id":       {"id-rider-a", "rider-a"},
			"someone with no profile":                 {"id-nobody", "driver-a"},
			"a driver naming no one":                  {"id-driver-a", ""},
			"a driver naming a driver that is absent": {"id-driver-a", "no-such-driver"},
			"someone with both profiles as another":   {"id-both", "driver-a"},
		}

		for who, tc := range denied {
			if code := callOwnershipAs(t, tc.identity, ownershipProfiles, ownershipTrips, or.name, or.req(tc.driver)); code != codes.PermissionDenied {
				t.Errorf("%s / %s: expected PermissionDenied, got %v", or.name, who, code)
			}
		}
	}
}

func TestOnlyDispatchMayOfferATrip(t *testing.T) {
	offer := &tripv1.OfferTripRequest{TripId: "trip-1", DriverId: "driver-a", TtlSeconds: 15}

	for name, identity := range map[string]string{
		"the driver being offered": "id-driver-a",
		"another driver":           "id-driver-b",
		"the rider of the trip":    "id-rider-a",
		"someone with no profile":  "id-nobody",
	} {
		if code := callOwnershipAs(t, identity, ownershipProfiles, ownershipTrips, "OfferTrip", offer); code != codes.PermissionDenied {
			t.Errorf("%s: expected PermissionDenied, got %v", name, code)
		}
	}

	if code := callOwnershipAs(t, internalServicePrincipalID, ownershipProfiles, ownershipTrips, "OfferTrip", offer); code != codes.OK {
		t.Errorf("the internal service: expected OK, got %v", code)
	}
}

func TestTheInternalServiceMayAnswerForAnyDriver(t *testing.T) {
	for _, or := range offerRequests {
		if code := callOwnershipAs(t, internalServicePrincipalID, ownershipProfiles, ownershipTrips, or.name, or.req("driver-a")); code != codes.OK {
			t.Errorf("%s: expected OK for the internal service, got %v", or.name, code)
		}
	}
}

func TestAnOfferCheckNeedsARequestThatNamesADriver(t *testing.T) {
	wrong := &tripv1.GetTripRequest{TripId: "trip-1"}

	for _, or := range offerRequests {
		if code := callOwnershipAs(t, "id-driver-a", ownershipProfiles, ownershipTrips, or.name, wrong); code != codes.PermissionDenied {
			t.Errorf("%s: expected PermissionDenied, got %v", or.name, code)
		}
	}
}
