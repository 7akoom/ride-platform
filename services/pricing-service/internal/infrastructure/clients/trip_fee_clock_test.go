package clients

import (
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTheCancellationFeeCountsFromWhenTheDriverSetOff(t *testing.T) {
	accepted := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	setOff := accepted.Add(9 * time.Minute)

	cases := map[string]struct {
		trip *tripv1.Trip
		want *time.Time
	}{
		"an ordinary trip": {
			&tripv1.Trip{AcceptedAt: timestamppb.New(accepted), HeadingToPickupAt: timestamppb.New(accepted)}, &accepted,
		},
		"a trip from before next trips": {
			&tripv1.Trip{AcceptedAt: timestamppb.New(accepted)}, &accepted,
		},
		"a next trip the driver set off for": {
			&tripv1.Trip{AcceptedAt: timestamppb.New(accepted), HeadingToPickupAt: timestamppb.New(setOff)}, &setOff,
		},
		"a next trip the driver had not set off for": {
			&tripv1.Trip{AcceptedAt: timestamppb.New(accepted), WaitingForPreviousTrip: true}, nil,
		},
	}

	for name, c := range cases {
		got := feeClockStart(c.trip)

		switch {
		case c.want == nil && got != nil:
			t.Errorf("%s: %v, want none", name, got)
		case c.want != nil && (got == nil || !got.Equal(*c.want)):
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
