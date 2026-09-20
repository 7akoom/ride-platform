package grpc

import (
	"context"
	"errors"
	"testing"

	notificationv1 "github.com/7akoom/ride-platform/gen/go/ride/notification/v1"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	typeRider  = notificationv1.RecipientType_RECIPIENT_TYPE_RIDER
	typeDriver = notificationv1.RecipientType_RECIPIENT_TYPE_DRIVER
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

type ownershipTestDevice struct {
	recipientType notification.RecipientType
	recipientID   string
}

type ownershipTestDevices struct {
	owners  map[string]ownershipTestDevice
	err     error
	lookups int
	asked   []string
}

func (m *ownershipTestDevices) DeviceOwner(_ context.Context, deviceToken string) (notification.RecipientType, string, bool, error) {
	m.lookups++
	m.asked = append(m.asked, deviceToken)

	if m.err != nil {
		return "", "", false, m.err
	}

	owner, ok := m.owners[deviceToken]

	return owner.recipientType, owner.recipientID, ok, nil
}

func newOwnershipDevices() *ownershipTestDevices {
	return &ownershipTestDevices{owners: map[string]ownershipTestDevice{
		"token-rider-a":  {notification.RecipientRider, "rider-a"},
		"token-driver-a": {notification.RecipientDriver, "driver-a"},
		"token-broken":   {notification.RecipientType("alien"), "rider-a"},
	}}
}

func callOwnershipAs(t *testing.T, identity string, resolver CallerResolver, devices DeviceOwnerReader, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(resolver, devices)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identity,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: notificationRPCPrefix + method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

// recipientRequests builds the three recipient-addressed requests for one recipient.
func recipientRequests(recipientType notificationv1.RecipientType, recipientID string) map[string]any {
	return map[string]any{
		"RegisterDevice":    &notificationv1.RegisterDeviceRequest{RecipientType: recipientType, RecipientId: recipientID, DeviceToken: "t"},
		"ListNotifications": &notificationv1.ListNotificationsRequest{RecipientType: recipientType, RecipientId: recipientID},
		"MarkAsRead":        &notificationv1.MarkAsReadRequest{RecipientType: recipientType, RecipientId: recipientID},
	}
}

func expectRecipientCodes(t *testing.T, identity string, want codes.Code, requests map[string]any) {
	t.Helper()

	for method, request := range requests {
		if code := callOwnershipAs(t, identity, newOwnershipResolver(), newOwnershipDevices(), method, request); code != want {
			t.Errorf("%s as %s: expected %v, got %v", method, identity, want, code)
		}
	}
}

func unregister(token string) *notificationv1.UnregisterDeviceRequest {
	return &notificationv1.UnregisterDeviceRequest{DeviceToken: token}
}

func TestARiderManagesOnlyTheirOwnRecipient(t *testing.T) {
	expectRecipientCodes(t, "id-rider-a", codes.OK, recipientRequests(typeRider, "rider-a"))
	expectRecipientCodes(t, "id-rider-b", codes.PermissionDenied, recipientRequests(typeRider, "rider-a"))
}

func TestADriverManagesOnlyTheirOwnRecipient(t *testing.T) {
	expectRecipientCodes(t, "id-driver-a", codes.OK, recipientRequests(typeDriver, "driver-a"))
	expectRecipientCodes(t, "id-driver-b", codes.PermissionDenied, recipientRequests(typeDriver, "driver-a"))
}

func TestRecipientTypesCannotBeSwapped(t *testing.T) {
	// A driver naming their own driver id as a rider recipient, a rider naming
	// their rider id as a driver, and the two ids under each other's type.
	expectRecipientCodes(t, "id-driver-a", codes.PermissionDenied, recipientRequests(typeRider, "driver-a"))
	expectRecipientCodes(t, "id-rider-a", codes.PermissionDenied, recipientRequests(typeDriver, "rider-a"))
	expectRecipientCodes(t, "id-both", codes.PermissionDenied, recipientRequests(typeDriver, "rider-both"))
	expectRecipientCodes(t, "id-both", codes.PermissionDenied, recipientRequests(typeRider, "driver-both"))

	expectRecipientCodes(t, "id-both", codes.OK, recipientRequests(typeDriver, "driver-both"))
	expectRecipientCodes(t, "id-both", codes.OK, recipientRequests(typeRider, "rider-both"))
}

func TestAnIdentityWithoutAProfileIsDenied(t *testing.T) {
	expectRecipientCodes(t, "id-nobody", codes.PermissionDenied, recipientRequests(typeRider, "rider-a"))
	expectRecipientCodes(t, "id-nobody", codes.PermissionDenied, recipientRequests(typeDriver, "driver-a"))
}

func TestUnspecifiedRecipientTypeAndEmptyIDsAreDeniedWithoutALookup(t *testing.T) {
	resolver := newOwnershipResolver()
	devices := newOwnershipDevices()

	for method, request := range recipientRequests(notificationv1.RecipientType_RECIPIENT_TYPE_UNSPECIFIED, "rider-a") {
		if code := callOwnershipAs(t, "id-rider-a", resolver, devices, method, request); code != codes.PermissionDenied {
			t.Errorf("%s with an unspecified type: expected PermissionDenied, got %v", method, code)
		}
	}

	for method, request := range recipientRequests(typeRider, "") {
		if code := callOwnershipAs(t, "id-rider-a", resolver, devices, method, request); code != codes.PermissionDenied {
			t.Errorf("%s with an empty recipient id: expected PermissionDenied, got %v", method, code)
		}
	}

	if resolver.lookups != 0 {
		t.Errorf("unusable requests caused %d profile lookup(s), expected none", resolver.lookups)
	}
}

func TestARecipientIDWithPaddingIsDenied(t *testing.T) {
	// The service trims recipient ids; the check compares exactly, so padding
	// can never turn someone else's id into the caller's own.
	expectRecipientCodes(t, "id-rider-a", codes.PermissionDenied, recipientRequests(typeRider, " rider-a "))
}

func TestOwnershipIsUnavailableNotAllowedWhenTheLookupFails(t *testing.T) {
	resolver := newOwnershipResolver()
	resolver.err = errors.New("rider-service down")

	for method, request := range recipientRequests(typeRider, "rider-a") {
		if code := callOwnershipAs(t, "id-rider-a", resolver, newOwnershipDevices(), method, request); code != codes.Unavailable {
			t.Errorf("%s with a failing resolver: expected Unavailable, got %v", method, code)
		}
	}
}

func TestADeviceCanBeUnregisteredOnlyByItsOwner(t *testing.T) {
	cases := []struct {
		name     string
		identity string
		token    string
		want     codes.Code
	}{
		{"the rider unregisters their own device", "id-rider-a", "token-rider-a", codes.OK},
		{"another rider unregisters it", "id-rider-b", "token-rider-a", codes.PermissionDenied},
		{"a driver unregisters a rider's device", "id-driver-a", "token-rider-a", codes.PermissionDenied},
		{"the driver unregisters their own device", "id-driver-a", "token-driver-a", codes.OK},
		{"a rider unregisters a driver's device", "id-rider-a", "token-driver-a", codes.PermissionDenied},
		{"another driver unregisters it", "id-driver-b", "token-driver-a", codes.PermissionDenied},
		{"a token nobody registered", "id-rider-a", "token-unknown", codes.PermissionDenied},
		{"a device with a corrupt recipient type", "id-rider-a", "token-broken", codes.PermissionDenied},
		{"an identity with no profile", "id-nobody", "token-rider-a", codes.PermissionDenied},
		{"the owner, with padding around the token", "id-rider-a", "  token-rider-a ", codes.OK},
		{"a stranger, with padding around the token", "id-rider-b", "  token-rider-a ", codes.PermissionDenied},
	}

	for _, tc := range cases {
		if code := callOwnershipAs(t, tc.identity, newOwnershipResolver(), newOwnershipDevices(), "UnregisterDevice", unregister(tc.token)); code != tc.want {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.want, code)
		}
	}
}

func TestAnEmptyTokenIsDeniedWithoutALookup(t *testing.T) {
	for _, token := range []string{"", "   ", "\t\n"} {
		devices := newOwnershipDevices()

		if code := callOwnershipAs(t, "id-rider-a", newOwnershipResolver(), devices, "UnregisterDevice", unregister(token)); code != codes.PermissionDenied {
			t.Errorf("token %q: expected PermissionDenied, got %v", token, code)
		}

		if devices.lookups != 0 {
			t.Errorf("token %q reached the repository %d time(s); it must be denied first", token, devices.lookups)
		}
	}
}

func TestTheTokenIsLookedUpTrimmed(t *testing.T) {
	devices := newOwnershipDevices()

	callOwnershipAs(t, "id-rider-a", newOwnershipResolver(), devices, "UnregisterDevice", unregister("  token-rider-a "))

	if len(devices.asked) != 1 || devices.asked[0] != "token-rider-a" {
		t.Errorf("expected one lookup of the trimmed token, got %q", devices.asked)
	}
}

func TestUnregisterIsUnavailableNotAllowedWhenALookupFails(t *testing.T) {
	brokenDevices := newOwnershipDevices()
	brokenDevices.err = errors.New("database down")

	if code := callOwnershipAs(t, "id-rider-a", newOwnershipResolver(), brokenDevices, "UnregisterDevice", unregister("token-rider-a")); code != codes.Unavailable {
		t.Errorf("a failing device lookup: expected Unavailable, got %v", code)
	}

	brokenResolver := newOwnershipResolver()
	brokenResolver.err = errors.New("rider-service down")

	if code := callOwnershipAs(t, "id-rider-a", brokenResolver, newOwnershipDevices(), "UnregisterDevice", unregister("token-rider-a")); code != codes.Unavailable {
		t.Errorf("a failing profile lookup: expected Unavailable, got %v", code)
	}
}

func TestNoReaderMeansNoAccess(t *testing.T) {
	if code := callOwnershipAs(t, "id-rider-a", newOwnershipResolver(), nil, "UnregisterDevice", unregister("token-rider-a")); code != codes.PermissionDenied {
		t.Errorf("no device reader: expected PermissionDenied, got %v", code)
	}

	for method, request := range recipientRequests(typeRider, "rider-a") {
		if code := callOwnershipAs(t, "id-rider-a", nil, newOwnershipDevices(), method, request); code != codes.PermissionDenied {
			t.Errorf("%s with no resolver: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestWrongRequestTypeIsDenied(t *testing.T) {
	wrong := &notificationv1.SendRequest{RecipientType: typeRider, RecipientId: "rider-a"}

	for _, method := range []string{"RegisterDevice", "UnregisterDevice", "ListNotifications", "MarkAsRead"} {
		if code := callOwnershipAs(t, "id-rider-a", newOwnershipResolver(), newOwnershipDevices(), method, wrong); code != codes.PermissionDenied {
			t.Errorf("%s with the wrong request type: expected PermissionDenied, got %v", method, code)
		}
	}
}

func TestSendAndTemplatesAreInternalOnly(t *testing.T) {
	for _, method := range []string{"Send", "UpsertTemplate"} {
		for _, identity := range []string{"id-rider-a", "id-driver-a", "id-both", "id-nobody"} {
			if code := callOwnershipAs(t, identity, newOwnershipResolver(), newOwnershipDevices(), method, nil); code != codes.PermissionDenied {
				t.Errorf("%s as %s: expected PermissionDenied, got %v", method, identity, code)
			}
		}

		if code := callOwnershipAs(t, internalServicePrincipalID, newOwnershipResolver(), newOwnershipDevices(), method, nil); code != codes.OK {
			t.Errorf("%s as the internal caller: expected OK, got %v", method, code)
		}
	}
}

func TestInternalCallerReachesEverythingWithoutALookup(t *testing.T) {
	resolver := newOwnershipResolver()
	devices := newOwnershipDevices()

	requests := recipientRequests(typeDriver, "driver-b")
	requests["UnregisterDevice"] = unregister("token-rider-a")

	for method, request := range requests {
		if code := callOwnershipAs(t, internalServicePrincipalID, resolver, devices, method, request); code != codes.OK {
			t.Errorf("%s as the internal caller: expected OK, got %v", method, code)
		}
	}

	if resolver.lookups != 0 || devices.lookups != 0 {
		t.Errorf("the internal caller caused %d profile and %d device lookup(s), expected none", resolver.lookups, devices.lookups)
	}
}
