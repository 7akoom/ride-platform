package grpc

import (
	"context"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
)

const locationRPCPrefix = "/ride.location.v1.LocationService/"

// ownsEntity reports whether the caller is the entity a location belongs to:
// the driver whose position it is, or the rider. A driver id is never accepted
// as a rider entity or the other way round, and an unspecified entity type
// owns nothing.
func ownsEntity(ctx context.Context, c caller, entityType locationv1.EntityType, entityID string) (bool, error) {
	switch entityType {
	case locationv1.EntityType_ENTITY_TYPE_DRIVER:
		return c.ownsDriver(ctx, entityID)

	case locationv1.EntityType_ENTITY_TYPE_RIDER:
		return c.ownsRider(ctx, entityID)

	default:
		return false, nil
	}
}

var ownerChecks = map[string]ownerCheck{
	locationRPCPrefix + "UpdateLocation": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*locationv1.UpdateLocationRequest)
		if !ok {
			return false, nil
		}

		return ownsEntity(ctx, c, r.GetEntityType(), r.GetEntityId())
	},
	locationRPCPrefix + "GetLocation": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*locationv1.GetLocationRequest)
		if !ok {
			return false, nil
		}

		return ownsEntity(ctx, c, r.GetEntityType(), r.GetEntityId())
	},
}
