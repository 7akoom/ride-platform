package grpc

import "context"

// profileIDs is what GetActiveTrip and ListTrips carry to say whose trips they
// want: the caller's own rider profile or their own driver profile.
type profileIDs interface {
	GetRiderId() string
	GetDriverId() string
}

// ownerOfProfile passes a request that names exactly one profile, and only when
// that profile is the caller's own. Naming both, or neither, is refused here, so
// nothing behind this check ever has to guess whose trips are meant. It answers
// the same way for a profile that belongs to someone else and one that does not
// exist.
func ownerOfProfile(ctx context.Context, c caller, request any) (bool, error) {
	ids, ok := request.(profileIDs)
	if !ok {
		return false, nil
	}

	riderID, driverID := ids.GetRiderId(), ids.GetDriverId()

	switch {
	case riderID != "" && driverID == "":
		return c.ownsRider(ctx, riderID)

	case driverID != "" && riderID == "":
		return c.ownsDriver(ctx, driverID)

	default:
		return false, nil
	}
}
