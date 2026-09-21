package grpc

import "testing"

// Approving or rejecting a driver is an operator action. If either of these
// ever becomes accessOwner or accessAuthenticated, a driver could approve
// themselves, so the level is pinned here.
func TestApprovalRPCsAreInternalOnly(t *testing.T) {
	for _, method := range []string{
		driverRPCPrefix + "ApproveDriver",
		driverRPCPrefix + "RejectDriver",
	} {
		level, classified := methodAccess[method]
		if !classified {
			t.Errorf("%s is not classified in methodAccess", method)

			continue
		}

		if level != accessInternal {
			t.Errorf("%s has access level %d, want accessInternal", method, level)
		}
	}
}
