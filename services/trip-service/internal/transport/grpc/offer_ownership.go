package grpc

import "context"

// driverIDGetter is what the driver's answers to an offer carry: which driver is
// answering.
type driverIDGetter interface{ GetDriverId() string }

// ownerOfDriverID passes a request that names a driver only when that driver is the
// caller's own driver profile. It is enough for reading and answering an offer: the
// offer is looked up by (trip, driver), so a driver can only ever reach an offer
// that was made to them, and one that does not exist answers the same as one made
// to somebody else.
func ownerOfDriverID(ctx context.Context, c caller, request any) (bool, error) {
	getter, ok := request.(driverIDGetter)
	if !ok {
		return false, nil
	}

	return c.ownsDriver(ctx, getter.GetDriverId())
}
