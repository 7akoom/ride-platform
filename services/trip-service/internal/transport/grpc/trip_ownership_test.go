package grpc

import (
	"context"
	"errors"
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ownershipTestResolver struct {
	riders  map[string]string
	drivers map[string]string
	err     error
}

func (m ownershipTestResolver) RiderID(_ context.Context, identityID string) (string, error) {
	return m.riders[identityID], m.err
}

func (m ownershipTestResolver) DriverID(_ context.Context, identityID string) (string, error) {
	return m.drivers[identityID], m.err
}

type ownershipTestTrips struct {
	trips map[string]trip.Trip
	err   error
}

func (m ownershipTestTrips) GetTrip(_ context.Context, tripID string) (trip.Trip, error) {
	if m.err != nil {
		return trip.Trip{}, m.err
	}

	found, ok := m.trips[tripID]
	if !ok {
		return trip.Trip{}, trip.ErrTripNotFound
	}

	return found, nil
}

var ownershipProfiles = ownershipTestResolver{
	riders:  map[string]string{"id-rider-a": "rider-a", "id-rider-b": "rider-b", "id-both": "rider-both"},
	drivers: map[string]string{"id-driver-a": "driver-a", "id-driver-b": "driver-b", "id-both": "driver-both"},
}

var ownershipTrips = ownershipTestTrips{trips: map[string]trip.Trip{
	"trip-1":    {ID: "trip-1", RiderID: "rider-a", DriverID: "driver-a"},
	"trip-open": {ID: "trip-open", RiderID: "rider-a"},
}}

func callOwnershipAs(t *testing.T, identity string, resolver CallerResolver, trips TripReader, method string, request any) codes.Code {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(resolver, trips)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identity,
		SessionID:  "session-1",
	})

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: tripRPCPrefix + method}, func(context.Context, any) (any, error) {
		return nil, nil
	})

	return status.Code(err)
}

type ownershipCase struct {
	name     string
	identity string
	method   string
	request  any
}

func ownershipTripRequests(tripID string) map[string]any {
	return map[string]any{
		"StartTrip":      &tripv1.StartTripRequest{TripId: tripID},
		"CompleteTrip":   &tripv1.CompleteTripRequest{TripId: tripID},
		"CancelTrip":     &tripv1.CancelTripRequest{TripId: tripID},
		"GetTrip":        &tripv1.GetTripRequest{TripId: tripID},
		"RecordWaypoint": &tripv1.RecordWaypointRequest{TripId: tripID},
		"GetTripPath":    &tripv1.GetTripPathRequest{TripId: tripID},
	}
}

func runOwnershipCases(t *testing.T, want codes.Code, cases []ownershipCase) {
	t.Helper()

	for _, tc := range cases {
		if code := callOwnershipAs(t, tc.identity, ownershipProfiles, ownershipTrips, tc.method, tc.request); code != want {
			t.Errorf("%s: expected %v, got %v", tc.name, want, code)
		}
	}
}

func TestTripRiderReachesOnlyTheRiderActions(t *testing.T) {
	requests := ownershipTripRequests("trip-1")

	runOwnershipCases(t, codes.OK, []ownershipCase{
		{"rider reads the trip", "id-rider-a", "GetTrip", requests["GetTrip"]},
		{"rider cancels the trip", "id-rider-a", "CancelTrip", requests["CancelTrip"]},
		{"rider reads the path", "id-rider-a", "GetTripPath", requests["GetTripPath"]},
		{"rider raises SOS as the rider", "id-rider-a", "TriggerSOS", &tripv1.TriggerSOSRequest{TripId: "trip-1", TriggeredBy: tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_RIDER}},
		{"rider requests a trip for themselves", "id-rider-a", "RequestTrip", &tripv1.RequestTripRequest{RiderId: "rider-a"}},
	})

	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"rider starts the trip", "id-rider-a", "StartTrip", requests["StartTrip"]},
		{"rider completes the trip", "id-rider-a", "CompleteTrip", requests["CompleteTrip"]},
		{"rider records a waypoint", "id-rider-a", "RecordWaypoint", requests["RecordWaypoint"]},
		{"rider raises SOS claiming to be the driver", "id-rider-a", "TriggerSOS", &tripv1.TriggerSOSRequest{TripId: "trip-1", TriggeredBy: tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_DRIVER}},
	})
}

func TestTripDriverReachesOnlyTheDriverActions(t *testing.T) {
	requests := ownershipTripRequests("trip-1")

	runOwnershipCases(t, codes.OK, []ownershipCase{
		{"driver reads the trip", "id-driver-a", "GetTrip", requests["GetTrip"]},
		{"driver starts the trip", "id-driver-a", "StartTrip", requests["StartTrip"]},
		{"driver completes the trip", "id-driver-a", "CompleteTrip", requests["CompleteTrip"]},
		{"driver cancels the trip", "id-driver-a", "CancelTrip", requests["CancelTrip"]},
		{"driver records a waypoint", "id-driver-a", "RecordWaypoint", requests["RecordWaypoint"]},
		{"driver reads the path", "id-driver-a", "GetTripPath", requests["GetTripPath"]},
		{"driver raises SOS as the driver", "id-driver-a", "TriggerSOS", &tripv1.TriggerSOSRequest{TripId: "trip-1", TriggeredBy: tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_DRIVER}},
	})

	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"driver raises SOS claiming to be the rider", "id-driver-a", "TriggerSOS", &tripv1.TriggerSOSRequest{TripId: "trip-1", TriggeredBy: tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_RIDER}},
		{"driver requests a trip as a rider", "id-driver-a", "RequestTrip", &tripv1.RequestTripRequest{RiderId: "rider-a"}},
	})
}

func TestTripStrangersAreDeniedEverything(t *testing.T) {
	requests := ownershipTripRequests("trip-1")

	var cases []ownershipCase

	for _, identity := range []string{"id-rider-b", "id-driver-b", "id-nobody"} {
		for method, request := range requests {
			cases = append(cases, ownershipCase{identity + " " + method, identity, method, request})
		}

		cases = append(cases,
			ownershipCase{identity + " SOS as rider", identity, "TriggerSOS", &tripv1.TriggerSOSRequest{TripId: "trip-1", TriggeredBy: tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_RIDER}},
			ownershipCase{identity + " SOS as driver", identity, "TriggerSOS", &tripv1.TriggerSOSRequest{TripId: "trip-1", TriggeredBy: tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_DRIVER}},
			ownershipCase{identity + " requests a trip for rider A", identity, "RequestTrip", &tripv1.RequestTripRequest{RiderId: "rider-a"}},
		)
	}

	runOwnershipCases(t, codes.PermissionDenied, cases)
}

func TestTripRiderCanRequestOnlyForThemselves(t *testing.T) {
	runOwnershipCases(t, codes.OK, []ownershipCase{
		{"rider B requests for rider B", "id-rider-b", "RequestTrip", &tripv1.RequestTripRequest{RiderId: "rider-b"}},
	})

	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"rider B requests for rider A", "id-rider-b", "RequestTrip", &tripv1.RequestTripRequest{RiderId: "rider-a"}},
		{"an empty rider id", "id-rider-b", "RequestTrip", &tripv1.RequestTripRequest{}},
	})
}

func TestTripWithoutADriverHasNoDriverToAct(t *testing.T) {
	requests := ownershipTripRequests("trip-open")

	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"a driver starts an unassigned trip", "id-driver-a", "StartTrip", requests["StartTrip"]},
		{"a driver reads an unassigned trip", "id-driver-a", "GetTrip", requests["GetTrip"]},
	})

	runOwnershipCases(t, codes.OK, []ownershipCase{
		{"the rider cancels their unassigned trip", "id-rider-a", "CancelTrip", requests["CancelTrip"]},
	})
}

func TestTripMissingTripsLookLikeSomeoneElsesTrips(t *testing.T) {
	requests := ownershipTripRequests("no-such-trip")

	var cases []ownershipCase

	for method, request := range requests {
		cases = append(cases, ownershipCase{"missing trip " + method, "id-rider-a", method, request})
	}

	runOwnershipCases(t, codes.PermissionDenied, cases)
}

func TestTripSomeoneWhoIsBothActsOnlyInTheirRoleOnTheTrip(t *testing.T) {
	requests := ownershipTripRequests("trip-1")

	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"rider-and-driver is not on trip-1", "id-both", "GetTrip", requests["GetTrip"]},
	})
}

func TestTripAcceptStaysInternalOnly(t *testing.T) {
	runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
		{"the driver accepts a trip", "id-driver-a", "AcceptTrip", &tripv1.AcceptTripRequest{TripId: "trip-open", DriverId: "driver-a"}},
		{"a rider accepts a trip", "id-rider-a", "AcceptTrip", &tripv1.AcceptTripRequest{TripId: "trip-open", DriverId: "driver-a"}},
	})
}

func TestTripOwnershipIsUnavailableNotAllowedWhenLookupsFail(t *testing.T) {
	request := &tripv1.GetTripRequest{TripId: "trip-1"}

	brokenTrips := ownershipTestTrips{err: errors.New("database down")}
	if code := callOwnershipAs(t, "id-rider-a", ownershipProfiles, brokenTrips, "GetTrip", request); code != codes.Unavailable {
		t.Errorf("trip lookup failure: expected Unavailable, got %v", code)
	}

	brokenProfiles := ownershipTestResolver{err: errors.New("rider-service down")}
	if code := callOwnershipAs(t, "id-rider-a", brokenProfiles, ownershipTrips, "GetTrip", request); code != codes.Unavailable {
		t.Errorf("profile lookup failure: expected Unavailable, got %v", code)
	}
}

func TestTripRequestWithoutATripIDIsDenied(t *testing.T) {
	request := &tripv1.RequestTripRequest{RiderId: "rider-a"}

	if code := callOwnershipAs(t, "id-rider-a", ownershipProfiles, ownershipTrips, "GetTrip", request); code != codes.PermissionDenied {
		t.Errorf("a request that carries no trip id must be denied, got %v", code)
	}
}
