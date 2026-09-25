package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/schedule"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestThePassengersPhoneIsShownOnlyWhileTheTripIsUnderWay(t *testing.T) {
	for status, shown := range map[trip.Status]bool{
		trip.StatusRequested:  true,
		trip.StatusAccepted:   true,
		trip.StatusInProgress: true,
		trip.StatusCompleted:  false,
		trip.StatusCancelled:  false,
	} {
		out := toProtoTrip(trip.Trip{Status: status, PassengerName: "Sara", PassengerPhone: "+9647500000002", Scheduled: true})

		if (out.GetPassengerPhone() != "") != shown || out.GetPassengerName() != "Sara" || !out.GetScheduled() {
			t.Errorf("%s: %+v", status, out)
		}
	}
}

func TestScheduleErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &TripHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		schedule.ErrNotFound:             codes.NotFound,
		schedule.ErrKeyReused:            codes.AlreadyExists,
		schedule.ErrTooManyUpcoming:      codes.FailedPrecondition,
		schedule.ErrNotScheduled:         codes.FailedPrecondition,
		schedule.ErrTooSoon:              codes.InvalidArgument,
		schedule.ErrTooFar:               codes.InvalidArgument,
		schedule.ErrIdempotencyKey:       codes.InvalidArgument,
		trip.ErrInvalidPassenger:         codes.InvalidArgument,
		trip.ErrPickupOutsideServiceZone: status.Code(handler.mapTripError(trip.ErrPickupOutsideServiceZone)),
	} {
		if got := status.Code(handler.mapScheduleError(err)); got != want {
			t.Errorf("%v: got %v, want %v", err, got, want)
		}
	}

	if _, err := handler.ScheduleTrip(context.Background(), &tripv1.ScheduleTripRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("without schedules: %v", err)
	}
}

func TestOnlyTheRiderBooksListsAndCancelsTheirScheduledTrips(t *testing.T) {
	for _, c := range []struct {
		name     string
		identity string
		method   string
		request  any
		want     codes.Code
	}{
		{"the rider books", "id-rider-a", "ScheduleTrip", &tripv1.ScheduleTripRequest{RiderId: "rider-a"}, codes.OK},
		{"for another rider", "id-rider-a", "ScheduleTrip", &tripv1.ScheduleTripRequest{RiderId: "rider-b"}, codes.PermissionDenied},
		{"a driver books", "id-driver-a", "ScheduleTrip", &tripv1.ScheduleTripRequest{RiderId: "driver-a"}, codes.PermissionDenied},
		{"the rider lists", "id-rider-a", "ListScheduledTrips", &tripv1.ListScheduledTripsRequest{RiderId: "rider-a"}, codes.OK},
		{"another rider's list", "id-rider-a", "ListScheduledTrips", &tripv1.ListScheduledTripsRequest{RiderId: "rider-b"}, codes.PermissionDenied},
		{"the rider cancels", "id-rider-a", "CancelScheduledTrip", &tripv1.CancelScheduledTripRequest{RiderId: "rider-a", ScheduledTripId: "s-1"}, codes.OK},
		{"as another rider", "id-rider-a", "CancelScheduledTrip", &tripv1.CancelScheduledTripRequest{RiderId: "rider-b", ScheduledTripId: "s-1"}, codes.PermissionDenied},
	} {
		if got := callOwnershipAs(t, c.identity, ownershipProfiles, ownershipTrips, c.method, c.request); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
