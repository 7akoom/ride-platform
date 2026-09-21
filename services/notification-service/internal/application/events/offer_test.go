package events_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// offerSender records what the handler sends. The embedded interface is never called:
// only Send is used by the trip.offered path.
type offerSender struct {
	notification.Service

	sent []notification.SendInput
	err  error
}

func (s *offerSender) Send(_ context.Context, in notification.SendInput) (notification.SendResult, error) {
	s.sent = append(s.sent, in)

	return notification.SendResult{}, s.err
}

type offerTrips struct{ events.TripClient }

type offerDrivers struct{ events.DriverClient }

func newOfferHandler(sender *offerSender) *events.Handler {
	return events.NewHandler(
		sender,
		offerTrips{},
		offerDrivers{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func offerEvent(tripID, driverID string, expiresIn time.Duration) []byte {
	expires := time.Now().Add(expiresIn).UTC().Format(time.RFC3339Nano)

	return []byte(`{"event_id":"ev-1","event_type":"trip.offered","payload":{"trip_id":"` + tripID +
		`","driver_id":"` + driverID + `","offered_at":"2026-09-21T10:00:00Z","expires_at":"` + expires + `"}}`)
}

const (
	offeredTrip   = "3f2b6a7e-1c1d-4b5e-9a3f-0d8c6b1a2e4f"
	offeredDriver = "fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
)

func TestTripOffered_PushesToTheDriverWithoutAnythingAboutTheRider(t *testing.T) {
	sender := &offerSender{}

	if err := newOfferHandler(sender).Dispatch(context.Background(), "trip.offered", offerEvent(offeredTrip, offeredDriver, 15*time.Second)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("notifications sent = %d, want 1", len(sender.sent))
	}

	got := sender.sent[0]

	if got.RecipientType != notification.RecipientDriver || got.RecipientID != offeredDriver {
		t.Errorf("sent to %v %q, want the driver %q", got.RecipientType, got.RecipientID, offeredDriver)
	}

	if got.EventKey != "trip.offer_received" {
		t.Errorf("event key = %q, want trip.offer_received", got.EventKey)
	}

	if got.IdempotencyKey != "ev-1" {
		t.Errorf("idempotency key = %q, want the event id, so a redelivery cannot push twice", got.IdempotencyKey)
	}

	if got.Data["trip_id"] != offeredTrip || got.Data["type"] != "trip.offer_received" {
		t.Errorf("push data = %v, want the trip id and the kind of push", got.Data)
	}

	allowed := map[string]bool{"type": true, "trip_id": true, "expires_at": true}
	for key := range got.Data {
		if !allowed[key] {
			t.Errorf("push data carries %q, which the driver's lock screen should not see", key)
		}
	}
}

func TestTripOffered_AnExpiredOfferIsNotPushed(t *testing.T) {
	sender := &offerSender{}

	if err := newOfferHandler(sender).Dispatch(context.Background(), "trip.offered", offerEvent(offeredTrip, offeredDriver, -time.Second)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(sender.sent) != 0 {
		t.Errorf("an expired offer was pushed: %v", sender.sent)
	}
}

func TestTripOffered_WhatCanNeverBeSentIsAckedNotRetried(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"a payload that is not an object", []byte(`{"event_id":"ev-1","payload":"nope"}`)},
		{"no driver", offerEvent(offeredTrip, "", time.Minute)},
		{"no trip", offerEvent("", offeredDriver, time.Minute)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &offerSender{}

			if err := newOfferHandler(sender).Dispatch(context.Background(), "trip.offered", tc.data); err != nil {
				t.Fatalf("Dispatch returned %v, want nil (acked)", err)
			}

			if len(sender.sent) != 0 {
				t.Errorf("sent %v, want nothing", sender.sent)
			}
		})
	}
}

func TestTripOffered_AFailureToSendIsRetriedWhileTheOfferIsLive(t *testing.T) {
	boom := errors.New("notification database is down")
	sender := &offerSender{err: boom}

	err := newOfferHandler(sender).Dispatch(context.Background(), "trip.offered", offerEvent(offeredTrip, offeredDriver, time.Minute))
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the send error, so the event is redelivered", err)
	}
}
