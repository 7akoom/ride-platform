package grpc

import (
	"context"
	"errors"
	"regexp"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

const driverRPCPrefix = "/ride.driver.v1.DriverService/"

// DriverReader is the one lookup the owner checks need. driver.Service already
// satisfies it, so ownership is verified against this service's own data.
type DriverReader interface {
	GetDriver(ctx context.Context, driverID string) (driver.Driver, error)
}

// caller is the authenticated end user making the request.
type caller struct {
	identityID string
	drivers    DriverReader
}

// idShape is the canonical UUID form every profile id has. An id of any other
// shape cannot exist, so it is denied without a lookup.
var idShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ownsIdentity reports whether the request names the caller's own identity.
func (c caller) ownsIdentity(identityID string) bool {
	return c.identityID != "" && identityID == c.identityID
}

// ownsDriver reports whether the driver profile exists and belongs to the
// caller. A profile that does not exist counts as not owned, so callers cannot
// tell a missing profile from someone else's.
func (c caller) ownsDriver(ctx context.Context, driverID string) (bool, error) {
	if c.identityID == "" || c.drivers == nil || !idShape.MatchString(driverID) {
		return false, nil
	}

	found, err := c.drivers.GetDriver(ctx, driverID)
	if errors.Is(err, driver.ErrDriverNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return found.IdentityID != "" && found.IdentityID == c.identityID, nil
}

var ownerChecks = map[string]ownerCheck{
	driverRPCPrefix + "CreateDriver": func(_ context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*driverv1.CreateDriverRequest)
		if !ok {
			return false, nil
		}

		return c.ownsIdentity(r.GetIdentityId()), nil
	},
	driverRPCPrefix + "GetDriverByIdentity": func(_ context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*driverv1.GetDriverByIdentityRequest)
		if !ok {
			return false, nil
		}

		return c.ownsIdentity(r.GetIdentityId()), nil
	},
	driverRPCPrefix + "GetDriver": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*driverv1.GetDriverRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
	driverRPCPrefix + "UpdateDriverProfile": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*driverv1.UpdateDriverProfileRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
	driverRPCPrefix + "UpdateAvailability": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*driverv1.UpdateAvailabilityRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDriver(ctx, r.GetDriverId())
	},
}
