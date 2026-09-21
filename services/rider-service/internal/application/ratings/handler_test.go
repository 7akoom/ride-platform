package ratings_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/ratings"
)

type applierFake struct {
	calls []ratings.ApplyInput
	err   error
}

func (a *applierFake) ApplyRating(_ context.Context, in ratings.ApplyInput) error {
	a.calls = append(a.calls, in)

	return a.err
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const (
	ratingID = "3f2b6a7e-1c1d-4b5e-9a3f-0d8c6b1a2e4f"
	rateeID  = "fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
)

func event(ratedBy, stars, rating, ratee string) []byte {
	return []byte(`{"event_id":"e1","event_type":"trip.rated","payload":{"rating_id":"` + rating +
		`","trip_id":"t1","rated_by":"` + ratedBy + `","rater_id":"r1","ratee_id":"` + ratee +
		`","stars":"` + stars + `"}}`)
}

func newHandler(ratedBy string, applier *applierFake) *ratings.Handler {
	return ratings.NewHandler(applier, ratedBy, quietLogger())
}

func TestHandle_AppliesTheRatingsGivenByTheOtherSide(t *testing.T) {
	applier := &applierFake{}

	err := newHandler("driver", applier).Handle(context.Background(), ratings.SubjectTripRated,
		event("driver", "4", ratingID, rateeID))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(applier.calls) != 1 {
		t.Fatalf("applied %d ratings, want 1", len(applier.calls))
	}

	got := applier.calls[0]
	if got.RatingID != ratingID || got.RateeID != rateeID || got.Stars != 4 {
		t.Errorf("applied %+v, want rating %s about %s with 4 stars", got, ratingID, rateeID)
	}
}

func TestHandle_IgnoresWhatIsNotForThisService(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		data    []byte
	}{
		{"another subject", "trip.completed", event("driver", "4", ratingID, rateeID)},
		{"the rating given by this service's own side", ratings.SubjectTripRated, event("rider", "4", ratingID, rateeID)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applier := &applierFake{}

			if err := newHandler("driver", applier).Handle(context.Background(), tc.subject, tc.data); err != nil {
				t.Fatalf("Handle: %v", err)
			}

			if len(applier.calls) != 0 {
				t.Errorf("applied %v, want nothing", applier.calls)
			}
		})
	}
}

func TestHandle_DropsWhatCanNeverBeApplied(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"not JSON", []byte("garbage")},
		{"a payload that is not an object", []byte(`{"event_id":"e1","payload":"nope"}`)},
		{"zero stars", event("driver", "0", ratingID, rateeID)},
		{"six stars", event("driver", "6", ratingID, rateeID)},
		{"stars that are not a number", event("driver", "five", ratingID, rateeID)},
		{"a rating id that is not a UUID", event("driver", "4", "nope", rateeID)},
		{"a rated profile id that is not a UUID", event("driver", "4", ratingID, "nope")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applier := &applierFake{}

			// nil, not an error: redelivering it would fail the same way forever.
			if err := newHandler("driver", applier).Handle(context.Background(), ratings.SubjectTripRated, tc.data); err != nil {
				t.Fatalf("Handle returned %v, want nil (acked)", err)
			}

			if len(applier.calls) != 0 {
				t.Errorf("applied %v, want nothing", applier.calls)
			}
		})
	}
}

func TestHandle_AProfileThatDoesNotExistIsAckedNotRetried(t *testing.T) {
	applier := &applierFake{err: ratings.ErrProfileNotFound}

	err := newHandler("driver", applier).Handle(context.Background(), ratings.SubjectTripRated,
		event("driver", "4", ratingID, rateeID))
	if err != nil {
		t.Fatalf("Handle returned %v, want nil", err)
	}
}

func TestHandle_AFailureIsRetriedLaterNotInAHotLoop(t *testing.T) {
	boom := errors.New("database is down")
	applier := &applierFake{err: boom}

	err := newHandler("driver", applier).Handle(context.Background(), ratings.SubjectTripRated,
		event("driver", "4", ratingID, rateeID))
	if err == nil {
		t.Fatal("Handle returned nil, want an error so the event is redelivered")
	}

	if !errors.Is(err, boom) {
		t.Errorf("the error does not wrap the cause: %v", err)
	}

	delayed, ok := err.(interface{ RetryDelay() time.Duration })
	if !ok {
		t.Fatal("the error does not ask for a delayed redelivery")
	}

	if delayed.RetryDelay() <= 0 {
		t.Errorf("retry delay = %s, want it positive", delayed.RetryDelay())
	}
}

func TestNewHandler_RejectsBadArguments(t *testing.T) {
	cases := map[string]func(){
		"no applier":      func() { ratings.NewHandler(nil, "rider", quietLogger()) },
		"no logger":       func() { ratings.NewHandler(&applierFake{}, "rider", nil) },
		"an unknown side": func() { ratings.NewHandler(&applierFake{}, "admin", quietLogger()) },
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()

			build()
		})
	}
}
