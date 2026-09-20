package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
)

const tripRPCPrefix = "/ride.trip.v1.TripService/"

type tripIDGetter interface{ GetTripId() string }

var ownerChecks = map[string]ownerCheck{
	tripRPCPrefix + "RequestTrip": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*tripv1.RequestTripRequest)
		if !ok {
			return false, nil
		}

		return c.ownsRider(ctx, r.GetRiderId())
	},
	tripRPCPrefix + "StartTrip":         driverOfTrip,
	tripRPCPrefix + "CompleteTrip":      driverOfTrip,
	tripRPCPrefix + "RecordWaypoint":    driverOfTrip,
	tripRPCPrefix + "CancelTrip":        participantOfTrip,
	tripRPCPrefix + "GetTrip":           participantOfTrip,
	tripRPCPrefix + "GetTripPath":       participantOfTrip,
	tripRPCPrefix + "GetDriverLocation": riderOfTrip,
	tripRPCPrefix + "GetActiveTrip":     ownerOfProfile,
	tripRPCPrefix + "ListTrips":         ownerOfProfile,
	tripRPCPrefix + "TriggerSOS": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*tripv1.TriggerSOSRequest)
		if !ok {
			return false, nil
		}

		p, err := c.participation(ctx, r.GetTripId())
		if err != nil {
			return false, err
		}

		switch r.GetTriggeredBy() {
		case tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_RIDER:
			return p.rider, nil
		case tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_DRIVER:
			return p.driver, nil
		default:
			return false, nil
		}
	},
}

func driverOfTrip(ctx context.Context, c caller, request any) (bool, error) {
	getter, ok := request.(tripIDGetter)
	if !ok {
		return false, nil
	}

	p, err := c.participation(ctx, getter.GetTripId())

	return p.driver, err
}

func participantOfTrip(ctx context.Context, c caller, request any) (bool, error) {
	getter, ok := request.(tripIDGetter)
	if !ok {
		return false, nil
	}

	p, err := c.participation(ctx, getter.GetTripId())

	return p.rider || p.driver, err
}
