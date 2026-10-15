package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/walletpin"
)

const pinOwner = "22222222-2222-4222-8222-222222222222"

type handlerPINRepository struct {
	record walletpin.Record
	found  bool
}

func (r *handlerPINRepository) Find(context.Context, string) (walletpin.Record, bool, error) {
	return r.record, r.found, nil
}

func (r *handlerPINRepository) Update(
	_ context.Context,
	_ string,
	decide func(walletpin.Record, bool) walletpin.Change,
) (walletpin.Record, error) {
	change := decide(r.record, r.found)
	if change.Save {
		r.record, r.found = change.Record, true
	}

	return r.record, change.Err
}

func (r *handlerPINRepository) SessionStartedAt(context.Context, string, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

type handlerPINHasher struct{}

func (handlerPINHasher) Hash(identityID, pin string) (string, error) {
	return identityID + "/" + pin, nil
}

func (handlerPINHasher) Compare(hash, identityID, pin string) (bool, error) {
	return hash == identityID+"/"+pin, nil
}

type handlerPINDirectory struct{}

func (handlerPINDirectory) FindActiveByPhone(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (handlerPINDirectory) PhoneOf(context.Context, string) (string, error) { return "", nil }

func TestVerifyMyWalletPin(t *testing.T) {
	repo := &handlerPINRepository{
		record: walletpin.Record{IdentityID: pinOwner, PINHash: pinOwner + "/2580"},
		found:  true,
	}
	handler := NewWalletPinHandler(
		walletpin.NewService(repo, handlerPINHasher{}, handlerPINDirectory{}),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	owner := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: pinOwner,
		SessionID:  "33333333-3333-4333-8333-333333333333",
	})

	if _, err := handler.VerifyMyWalletPin(context.Background(), &identityv1.VerifyMyWalletPinRequest{Pin: "2580"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("without a person: %v", err)
	}

	wrong, err := handler.VerifyMyWalletPin(owner, &identityv1.VerifyMyWalletPinRequest{Pin: "1357"})
	if err != nil || wrong.GetResult() != identityv1.PinCheck_PIN_CHECK_WRONG || wrong.GetAttemptsLeft() != walletpin.MaxFailedAttempts-1 {
		t.Fatalf("a wrong PIN: %v, %v", wrong, err)
	}

	if repo.record.FailedAttempts != 1 {
		t.Fatalf("the wrong PIN was not counted: %+v", repo.record)
	}

	right, err := handler.VerifyMyWalletPin(owner, &identityv1.VerifyMyWalletPinRequest{Pin: "2580"})
	if err != nil || right.GetResult() != identityv1.PinCheck_PIN_CHECK_OK {
		t.Fatalf("the right PIN: %v, %v", right, err)
	}

	if repo.record.FailedAttempts != 0 {
		t.Fatalf("the right PIN did not clear the count: %+v", repo.record)
	}

	for i := 0; i < walletpin.MaxFailedAttempts; i++ {
		_, _ = handler.VerifyMyWalletPin(owner, &identityv1.VerifyMyWalletPinRequest{Pin: "1357"})
	}

	locked, err := handler.VerifyMyWalletPin(owner, &identityv1.VerifyMyWalletPinRequest{Pin: "2580"})
	if err != nil || locked.GetResult() != identityv1.PinCheck_PIN_CHECK_LOCKED || locked.GetLockedUntil() == nil {
		t.Fatalf("after five wrong PINs, even the right one: %v, %v", locked, err)
	}
}
