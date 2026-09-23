package grpc

import "testing"

// Approving or rejecting a driver is an operator action. If either of these
// ever becomes accessOwner or accessAuthenticated, a driver could approve
// themselves, so the level and the permission are pinned here.
func TestApprovalRPCsNeedTheReviewPermission(t *testing.T) {
	for _, method := range []string{
		driverRPCPrefix + "ApproveDriver",
		driverRPCPrefix + "RejectDriver",
	} {
		level, classified := methodAccess[method]
		if !classified {
			t.Errorf("%s is not classified in methodAccess", method)

			continue
		}

		if level != accessStaff {
			t.Errorf("%s has access level %d, want accessStaff", method, level)
		}

		if staffPermissions[method] != "drivers.approve" {
			t.Errorf("%s needs %q, want drivers.approve", method, staffPermissions[method])
		}
	}
}

func TestEveryStaffMethodNamesAPermission(t *testing.T) {
	for method, level := range methodAccess {
		if level == accessStaff && staffPermissions[method] == "" {
			t.Errorf("%s is accessStaff but names no permission", method)
		}
	}

	for method := range staffPermissions {
		level := methodAccess[method]
		if level != accessStaff && level != accessOwner {
			t.Errorf("%s names a staff permission but is neither accessStaff nor accessOwner", method)
		}
	}
}
