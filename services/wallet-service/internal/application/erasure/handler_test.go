package erasure

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type recordingEraser struct {
	requested, erased []Account
	err               error
}

func (r *recordingEraser) Requested(_ context.Context, a Account) error {
	r.requested = append(r.requested, a)

	return r.err
}

func (r *recordingEraser) Erase(_ context.Context, a Account) error {
	r.erased = append(r.erased, a)

	return r.err
}

const (
	identityID = "11111111-1111-4111-8111-111111111111"
	riderID    = "22222222-2222-4222-8222-222222222222"
)

func event(payload string) []byte {
	return []byte(`{"event_id":"e1","event_type":"x","payload":` + payload + `}`)
}

func TestHandlerCallsTheEraser(t *testing.T) {
	eraser := &recordingEraser{}
	h := NewHandler(eraser, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := event(`{"identity_id":"` + identityID + `","rider_id":"` + riderID + `","driver_id":""}`)

	if err := h.Handle(context.Background(), SubjectDeletionRequested, body); err != nil {
		t.Fatal(err)
	}

	if err := h.Handle(context.Background(), SubjectDeleted, body); err != nil {
		t.Fatal(err)
	}

	want := Account{IdentityID: identityID, RiderID: riderID}
	if len(eraser.requested) != 1 || eraser.requested[0] != want || len(eraser.erased) != 1 || eraser.erased[0] != want {
		t.Fatalf("got %+v / %+v", eraser.requested, eraser.erased)
	}
}

func TestHandlerDropsBadEventsAndRetriesFailures(t *testing.T) {
	eraser := &recordingEraser{}
	h := NewHandler(eraser, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, bad := range [][]byte{[]byte("nope"), event(`"x"`), event(`{"identity_id":"not-an-id"}`), event(`{"identity_id":"` + identityID + `","rider_id":"x"}`)} {
		if err := h.Handle(context.Background(), SubjectDeleted, bad); err != nil {
			t.Fatalf("%s: %v", bad, err)
		}
	}

	if len(eraser.erased) != 0 {
		t.Fatalf("a bad event reached the eraser: %+v", eraser.erased)
	}

	eraser.err = errors.New("database down")

	err := h.Handle(context.Background(), SubjectDeleted, event(`{"identity_id":"`+identityID+`"}`))

	var delayed interface{ RetryDelay() time.Duration }
	if !errors.As(err, &delayed) || delayed.RetryDelay() <= 0 {
		t.Fatalf("want a delayed retry, got %v", err)
	}
}
