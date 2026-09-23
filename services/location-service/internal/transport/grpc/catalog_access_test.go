package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/place"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEveryStaffMethodNamesItsPermission(t *testing.T) {
	for method, level := range methodAccess {
		permission, named := staffPermissions[method]

		if level == accessStaff && (!named || permission == "") {
			t.Errorf("%s is accessStaff without a permission", method)
		}

		if level != accessStaff && named {
			t.Errorf("%s has a staff permission but is not accessStaff", method)
		}
	}
}

func TestCitiesAndPlacesAccess(t *testing.T) {
	resolver := newOwnershipResolver()

	for _, method := range []string{"ListCities", "GetCity", "ListPlaces", "GetPlace"} {
		if code := callOwnershipAs(t, "id-rider-a", resolver, method, nil); code != codes.OK {
			t.Errorf("%s as a user: expected OK, got %v", method, code)
		}
	}

	wants := map[string]string{
		"AdminListCities": "zones.manage", "CreateCity": "zones.manage",
		"UpdateCity": "zones.manage", "SetCityActive": "zones.manage",
		"AdminListPlaces": "places.manage", "CreatePlace": "places.manage",
		"UpdatePlace": "places.manage", "SetPlaceActive": "places.manage",
	}

	for method, permission := range wants {
		if got := staffPermissions["/ride.location.v1.LocationService/"+method]; got != permission {
			t.Errorf("%s needs %q, has %q", method, permission, got)
		}

		// Without staff-service allowing it, a user is refused.
		if code := callOwnershipAs(t, "id-rider-a", resolver, method, nil); code != codes.PermissionDenied {
			t.Errorf("%s as a user: expected PermissionDenied, got %v", method, code)
		}

		staff := &fakeStaff{allowed: true}
		if ran, err := callZoneMethod(t, staff, method, &locationv1.SetPlaceActiveRequest{PlaceId: "p-1"}); !ran || err != nil {
			t.Errorf("%s as allowed staff: ran=%v err=%v", method, ran, err)
		}

		if len(staff.asked) != 1 {
			t.Errorf("%s: staff-service asked %d times", method, len(staff.asked))
		}
	}
}

func TestStaffAuditTargetNamesThePlaceOrCity(t *testing.T) {
	if got := staffTargetOf(&locationv1.UpdatePlaceRequest{PlaceId: "p-1"}); got != "p-1" {
		t.Fatalf("place: %q", got)
	}

	if got := staffTargetOf(&locationv1.UpdateCityRequest{CityId: "c-1"}); got != "c-1" {
		t.Fatalf("city: %q", got)
	}
}

func TestCatalogIsUnimplementedUntilConfigured(t *testing.T) {
	h := &LocationHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if _, err := h.ListCities(context.Background(), &locationv1.ListCitiesRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("got %v", err)
	}
}

func TestPlaceCategoriesRoundTrip(t *testing.T) {
	if len(categoriesFromProto) != len(place.Categories) {
		t.Fatalf("%d proto categories for %d domain ones", len(categoriesFromProto), len(place.Categories))
	}

	for _, category := range place.Categories {
		protoCategory := categoryToProto(category)
		if back, ok := categoryFromProto(protoCategory); !ok || back != category {
			t.Errorf("%s does not round-trip", category)
		}
	}

	if _, ok := categoryFromProto(locationv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED); ok {
		t.Error("UNSPECIFIED must not be a category")
	}
}
