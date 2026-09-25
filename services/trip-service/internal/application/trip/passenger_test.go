package trip_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestAPassengerIsANameAndAPhoneTogether(t *testing.T) {
	for _, c := range []struct {
		name, phone string
		ok          bool
	}{
		{"", "", true},
		{" Sara ", " +9647500000002 ", true},
		{"Sara", "", false},
		{"", "+9647500000002", false},
		{"Sara", "07500000002", false},
		{"Sara", "+0647500000002", false},
		{strings.Repeat("س", 81), "+9647500000002", false},
		{strings.Repeat("س", 80), "+9647500000002", true},
	} {
		name, phone, err := trip.NormalizePassenger(c.name, c.phone)
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, trip.ErrInvalidPassenger)) {
			t.Errorf("%q %q: %v", c.name, c.phone, err)
		}

		if err == nil && (name != strings.TrimSpace(c.name) || phone != strings.TrimSpace(c.phone)) {
			t.Errorf("%q %q: trimmed to %q %q", c.name, c.phone, name, phone)
		}
	}
}

func TestATripForSomeoneElseKeepsThemAndABookingKeepsItsID(t *testing.T) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	svc := newService(repo)

	input := validRequestInput()
	input.PassengerName, input.PassengerPhone = "Sara", "+9647500000002"

	if _, err := svc.RequestTrip(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	made := repo.createCalls[0]
	if made.ID != "new-trip-id" || made.PassengerName != "Sara" || made.PassengerPhone != "+9647500000002" || made.Scheduled {
		t.Fatalf("created %+v", made)
	}

	booked := validRequestInput()
	booked.ScheduledTripID = "11111111-1111-4111-8111-111111111111"

	if _, err := svc.RequestTrip(context.Background(), booked); err != nil {
		t.Fatal(err)
	}

	if made := repo.createCalls[1]; made.ID != booked.ScheduledTripID || !made.Scheduled {
		t.Fatalf("a booking's trip %+v", made)
	}

	bad := validRequestInput()
	bad.PassengerName = "Sara"

	if _, err := svc.RequestTrip(context.Background(), bad); !errors.Is(err, trip.ErrInvalidPassenger) {
		t.Fatalf("half a passenger: %v", err)
	}

	odd := validRequestInput()
	odd.ScheduledTripID = "booking-1"

	if _, err := svc.RequestTrip(context.Background(), odd); !errors.Is(err, trip.ErrTripIDRequired) {
		t.Fatalf("not an id: %v", err)
	}
}
