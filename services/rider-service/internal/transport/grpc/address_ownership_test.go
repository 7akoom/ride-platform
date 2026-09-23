package grpc

import (
	"testing"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"google.golang.org/grpc/codes"
)

func addressRequests(riderID string) map[string]any {
	const addressID = "44444444-4444-4444-8444-444444444444"

	return map[string]any{
		"CreateSavedAddress": &riderv1.CreateSavedAddressRequest{RiderId: riderID},
		"ListSavedAddresses": &riderv1.ListSavedAddressesRequest{RiderId: riderID},
		"GetSavedAddress":    &riderv1.GetSavedAddressRequest{RiderId: riderID, AddressId: addressID},
		"UpdateSavedAddress": &riderv1.UpdateSavedAddressRequest{RiderId: riderID, AddressId: addressID},
		"DeleteSavedAddress": &riderv1.DeleteSavedAddressRequest{RiderId: riderID, AddressId: addressID},
	}
}

func TestARiderReachesOnlyTheirOwnSavedAddresses(t *testing.T) {
	riders := newOwnershipRiders()

	for method, request := range addressRequests(ownershipRiderA) {
		if code := callOwnershipAs(t, "identity-a", riders, method, request); code != codes.OK {
			t.Errorf("%s as the owner: %v", method, code)
		}

		if code := callOwnershipAs(t, "identity-b", riders, method, request); code != codes.PermissionDenied {
			t.Errorf("%s as another rider: %v", method, code)
		}
	}

	for method, request := range addressRequests(ownershipMissingRider) {
		if code := callOwnershipAs(t, "identity-a", riders, method, request); code != codes.PermissionDenied {
			t.Errorf("%s for a rider that does not exist: %v", method, code)
		}
	}
}

func TestServicesReadSavedAddresses(t *testing.T) {
	riders := newOwnershipRiders()

	for method, request := range addressRequests(ownershipRiderB) {
		if code := callOwnershipAs(t, internalServicePrincipalID, riders, method, request); code != codes.OK {
			t.Errorf("%s as the internal caller: %v", method, code)
		}
	}
}
