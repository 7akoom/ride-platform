package events

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type sosFakeAlerter struct {
	calls int
	err   error
}

func (f *sosFakeAlerter) Alert(_ context.Context, _ SOSAlert) error {
	f.calls++

	return f.err
}

func newSOSTestDelivery(alerter OperatorAlerter, logOutput io.Writer) *sosDelivery {
	delivery := newSOSDelivery(slog.New(slog.NewTextHandler(logOutput, nil)))
	delivery.alerter = alerter
	delivery.retryInterval = 15 * time.Second
	delivery.giveUpAfter = 30 * time.Minute
	delivery.now = func() time.Time { return sosTime.Add(time.Minute) }

	return delivery
}

func sosSampleAlert() SOSAlert {
	return SOSAlert{AlertID: "alert-1", TripID: "trip-1", TriggeredBy: "rider", TriggeredAt: sosTime}
}

func TestDeliverSendsTheAlert(t *testing.T) {
	fake := &sosFakeAlerter{}
	delivery := newSOSTestDelivery(fake, io.Discard)

	if err := delivery.deliver(context.Background(), "evt-1", sosSampleAlert()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if fake.calls != 1 {
		t.Fatalf("expected one alert, got %d", fake.calls)
	}
}

func TestDeliverNeverAlertsTheSameEventTwice(t *testing.T) {
	fake := &sosFakeAlerter{}
	delivery := newSOSTestDelivery(fake, io.Discard)

	for range 3 {
		if err := delivery.deliver(context.Background(), "evt-1", sosSampleAlert()); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	}

	if fake.calls != 1 {
		t.Fatalf("operators must be paged once per event, got %d pages", fake.calls)
	}
}

func TestDeliverAlertsEachDistinctEvent(t *testing.T) {
	fake := &sosFakeAlerter{}
	delivery := newSOSTestDelivery(fake, io.Discard)

	_ = delivery.deliver(context.Background(), "evt-1", sosSampleAlert())
	_ = delivery.deliver(context.Background(), "evt-2", sosSampleAlert())

	if fake.calls != 2 {
		t.Fatalf("expected two pages for two events, got %d", fake.calls)
	}
}

func TestDeliverRetriesLaterWhenEveryChannelFailed(t *testing.T) {
	fake := &sosFakeAlerter{err: errors.New("gateway down")}
	delivery := newSOSTestDelivery(fake, io.Discard)

	err := delivery.deliver(context.Background(), "evt-1", sosSampleAlert())

	var delayed interface{ RetryDelay() time.Duration }
	if !errors.As(err, &delayed) || delayed.RetryDelay() != 15*time.Second {
		t.Fatalf("expected a delayed retry of 15s, got %v", err)
	}

	if !errors.Is(err, fake.err) {
		t.Fatalf("the retry error must wrap the cause")
	}
}

func TestDeliverAfterAFailureStillSendsOnTheNextAttempt(t *testing.T) {
	fake := &sosFakeAlerter{err: errors.New("gateway down")}
	delivery := newSOSTestDelivery(fake, io.Discard)

	_ = delivery.deliver(context.Background(), "evt-1", sosSampleAlert())

	fake.err = nil

	if err := delivery.deliver(context.Background(), "evt-1", sosSampleAlert()); err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}

	if fake.calls != 2 {
		t.Fatalf("a failed attempt must not count as delivered, got %d attempts", fake.calls)
	}
}

func TestDeliverGivesUpOnceTheRetryWindowHasPassed(t *testing.T) {
	var logs bytes.Buffer

	fake := &sosFakeAlerter{err: errors.New("gateway down")}
	delivery := newSOSTestDelivery(fake, &logs)
	delivery.now = func() time.Time { return sosTime.Add(31 * time.Minute) }

	if err := delivery.deliver(context.Background(), "evt-1", sosSampleAlert()); err != nil {
		t.Fatalf("expected nil (ack) after giving up, got %v", err)
	}

	if !strings.Contains(logs.String(), "giving up") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("giving up on an SOS must be logged as an error:\n%s", logs.String())
	}
}

func TestDeliverWithoutAnyChannelIsLoudAndDoesNotRetry(t *testing.T) {
	var logs bytes.Buffer

	delivery := newSOSTestDelivery(nil, &logs)

	if err := delivery.deliver(context.Background(), "evt-1", sosSampleAlert()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if !strings.Contains(logs.String(), "NOBODY") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("an unconfigured SOS must be logged as an error:\n%s", logs.String())
	}
}

func TestDeliverRememberedIdsAreBounded(t *testing.T) {
	fake := &sosFakeAlerter{}
	delivery := newSOSTestDelivery(fake, io.Discard)

	for i := range deliveredMemory + 10 {
		delivery.remember(string(rune('a'+i%26)) + strings.Repeat("x", i))
	}

	if len(delivery.delivered) > deliveredMemory || len(delivery.order) > deliveredMemory {
		t.Fatalf("memory must stay bounded, got %d ids", len(delivery.delivered))
	}
}
