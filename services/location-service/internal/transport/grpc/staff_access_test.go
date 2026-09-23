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

type fakeStaff struct {
	allowed   bool
	err       error
	asked     []string
	targets   []string
	completed map[string]codes.Code
}

func (f *fakeStaff) Authorize(_ context.Context, identityID, permission, method, targetID string) (bool, string, error) {
	f.asked = append(f.asked, identityID+" "+permission+" "+method)
	f.targets = append(f.targets, targetID)

	if f.err != nil {
		return false, "", f.err
	}

	return f.allowed, "audit-1", nil
}

func (f *fakeStaff) Complete(_ context.Context, auditEntryID string, code codes.Code) {
	if f.completed == nil {
		f.completed = map[string]codes.Code{}
	}

	f.completed[auditEntryID] = code
}

func callZoneMethod(t *testing.T, staff StaffAuthorizer, method string, request any) (bool, error) {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(newOwnershipResolver(), staff)
	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: "identity-1", SessionID: "s"})

	ran := false

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: "/ride.location.v1.LocationService/" + method},
		func(context.Context, any) (any, error) {
			ran = true

			return nil, nil
		})

	return ran, err
}

func TestStaffWithZonesManageChangesZones(t *testing.T) {
	staff := &fakeStaff{allowed: true}

	ran, err := callZoneMethod(t, staff, "UpdateZone", &locationv1.UpdateZoneRequest{ZoneId: "zone-7"})
	if !ran || err != nil {
		t.Fatalf("ran=%v err=%v", ran, err)
	}

	if staff.asked[0] != "identity-1 zones.manage /ride.location.v1.LocationService/UpdateZone" || staff.targets[0] != "zone-7" {
		t.Fatalf("asked %v for %v", staff.asked, staff.targets)
	}

	if staff.completed["audit-1"] != codes.OK {
		t.Fatalf("outcome = %v", staff.completed["audit-1"])
	}
}

func TestZoneChangesAreDeniedOrFailClosed(t *testing.T) {
	if ran, err := callZoneMethod(t, &fakeStaff{allowed: false}, "CreateZone", &locationv1.CreateZoneRequest{}); ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("not allowed: ran=%v err=%v", ran, err)
	}

	if ran, err := callZoneMethod(t, &fakeStaff{err: errors.New("down")}, "SetZoneActive", &locationv1.SetZoneActiveRequest{}); ran || status.Code(err) != codes.Unavailable {
		t.Fatalf("staff-service down: ran=%v err=%v", ran, err)
	}
}

func TestEveryStaffMethodNamesAPermission(t *testing.T) {
	for method, level := range methodAccess {
		if level == accessStaff && staffPermissions[method] == "" {
			t.Errorf("%s is accessStaff but names no permission", method)
		}
	}

	for method := range staffPermissions {
		if methodAccess[method] != accessStaff {
			t.Errorf("%s names a staff permission but is not accessStaff", method)
		}
	}
}
