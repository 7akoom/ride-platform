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

type noDrivers struct{}

func (noDrivers) GetDriver(context.Context, string) (driver.Driver, error) {
	return driver.Driver{}, driver.ErrDriverNotFound
}

func callStaffMethod(
	t *testing.T,
	staff StaffAuthorizer,
	method string,
	request any,
	handlerErr error,
) (bool, bool, error) {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(noDrivers{}, staff)
	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: "identity-1", SessionID: "s"})

	var ran, staffCall bool

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		ran = true
		staffCall = isStaffCall(ctx)

		return nil, handlerErr
	})

	return ran, staffCall, err
}

func TestApprovalRunsForAllowedStaffAndRecordsTheOutcome(t *testing.T) {
	staff := &fakeStaff{allowed: true}

	ran, staffCall, err := callStaffMethod(t, staff, driverRPCPrefix+"ApproveDriver",
		&driverv1.ApproveDriverRequest{DriverId: "driver-9"}, status.Error(codes.NotFound, "no"))

	if !ran || !staffCall || status.Code(err) != codes.NotFound {
		t.Fatalf("ran=%v staffCall=%v err=%v", ran, staffCall, err)
	}

	if staff.asked[0] != "identity-1 drivers.approve "+driverRPCPrefix+"ApproveDriver" || staff.targets[0] != "driver-9" {
		t.Fatalf("asked %v for %v", staff.asked, staff.targets)
	}

	if staff.completed["audit-1"] != codes.NotFound {
		t.Fatalf("outcome = %v", staff.completed["audit-1"])
	}
}

func TestApprovalIsDeniedForOtherUsers(t *testing.T) {
	ran, _, err := callStaffMethod(t, &fakeStaff{allowed: false}, driverRPCPrefix+"RejectDriver", &driverv1.RejectDriverRequest{}, nil)
	if ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
}

func TestStaffAccessFailsClosed(t *testing.T) {
	ran, _, err := callStaffMethod(t, &fakeStaff{err: errors.New("down")}, driverRPCPrefix+"ListDrivers", &driverv1.ListDriversRequest{}, nil)
	if ran || status.Code(err) != codes.Unavailable {
		t.Fatalf("staff-service down: ran=%v err=%v", ran, err)
	}

	ran, _, err = callStaffMethod(t, nil, driverRPCPrefix+"ListDrivers", &driverv1.ListDriversRequest{}, nil)
	if ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("no authorizer: ran=%v err=%v", ran, err)
	}
}

func TestStaffMayReadADriverThatIsNotTheirs(t *testing.T) {
	staff := &fakeStaff{allowed: true}

	ran, staffCall, err := callStaffMethod(t, staff, driverRPCPrefix+"GetDriver",
		&driverv1.GetDriverRequest{DriverId: "00000000-0000-4000-8000-000000000001"}, nil)
	if !ran || !staffCall || err != nil {
		t.Fatalf("ran=%v staffCall=%v err=%v", ran, staffCall, err)
	}

	if staff.asked[0] != "identity-1 drivers.read "+driverRPCPrefix+"GetDriver" {
		t.Fatalf("asked %v", staff.asked)
	}

	ran, _, err = callStaffMethod(t, &fakeStaff{allowed: false}, driverRPCPrefix+"GetDriver",
		&driverv1.GetDriverRequest{DriverId: "00000000-0000-4000-8000-000000000001"}, nil)
	if ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("not staff: ran=%v err=%v", ran, err)
	}
}

func TestOwnerMethodsWithoutAStaffPermissionNeverAskStaff(t *testing.T) {
	staff := &fakeStaff{allowed: true}

	ran, _, err := callStaffMethod(t, staff, driverRPCPrefix+"UpdateAvailability",
		&driverv1.UpdateAvailabilityRequest{DriverId: "00000000-0000-4000-8000-000000000001"}, nil)
	if ran || status.Code(err) != codes.PermissionDenied || len(staff.asked) != 0 {
		t.Fatalf("ran=%v err=%v asked=%v", ran, err, staff.asked)
	}
}
