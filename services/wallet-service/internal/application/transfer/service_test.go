package transfer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	senderRider       = "rider-sender"
	senderIdentity    = "identity-sender"
	recipientRider    = "rider-recipient"
	recipientIdentity = "identity-recipient"
	recipientPhone    = "+9647701234567"
	senderPhone       = "+9647509876543"
)

type fakeStore struct {
	config  wallet.Config
	byKey   map[string]Transfer
	sent    []Record
	sendErr error
	list    []Transfer
	offsets [2]int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byKey: map[string]Transfer{},
		config: wallet.Config{
			CurrencyCode: "IQD", TransferMinAmount: dec(250), TransferMaxAmount: dec(100000),
			TransferDailyAmount: dec(200000), TransferDailyCount: 5,
		},
	}
}

func (s *fakeStore) Config(context.Context) (wallet.Config, error) { return s.config, nil }

func (s *fakeStore) FindByKey(_ context.Context, sender, key string) (Transfer, bool, error) {
	t, ok := s.byKey[sender+"/"+key]
	return t, ok, nil
}

func (s *fakeStore) Send(_ context.Context, record Record) (Transfer, wallet.Wallet, error) {
	s.sent = append(s.sent, record)
	if s.sendErr != nil {
		return Transfer{}, wallet.Wallet{}, s.sendErr
	}

	t := record.Transfer
	t.ID = "transfer-1"
	s.byKey[t.SenderRiderID+"/"+t.IdempotencyKey] = t

	return t, wallet.Wallet{ID: "wallet-1", Balance: dec(1000)}, nil
}

func (s *fakeStore) List(_ context.Context, _ string, offset, limit int) ([]Transfer, error) {
	s.offsets = [2]int{offset, limit}
	return s.list, nil
}

type fakeIdentity struct {
	pin       PINResult
	pinCalls  int
	phones    map[string]string // phone -> identity
	lookupErr error
}

func (f *fakeIdentity) VerifyPIN(context.Context, string, string) (PINResult, error) {
	f.pinCalls++
	return f.pin, nil
}

func (f *fakeIdentity) FindByPhone(_ context.Context, phone string) (string, bool, error) {
	if f.lookupErr != nil {
		return "", false, f.lookupErr
	}

	id, ok := f.phones[phone]

	return id, ok, nil
}

func (f *fakeIdentity) PhoneOf(context.Context, string) (string, error) { return senderPhone, nil }

type fakeRiders map[string]string

func (r fakeRiders) RiderID(_ context.Context, identityID string) (string, error) {
	return r[identityID], nil
}

func dec(v int64) decimal.Decimal { return decimal.NewFromInt(v) }

func newTestService() (*Service, *fakeStore, *fakeIdentity) {
	store := newFakeStore()
	identity := &fakeIdentity{
		pin:    PINResult{Check: PINOK},
		phones: map[string]string{recipientPhone: recipientIdentity, senderPhone: senderIdentity},
	}
	riders := fakeRiders{recipientIdentity: recipientRider, senderIdentity: senderRider}

	return NewService(store, identity, riders), store, identity
}

func validSend() SendInput {
	return SendInput{
		SenderRiderID: senderRider, SenderIdentityID: senderIdentity, RecipientPhone: recipientPhone,
		Amount: dec(5000), Note: " dinner ", PIN: "2580", IdempotencyKey: "key-1",
	}
}

func TestSendingMoney(t *testing.T) {
	service, store, _ := newTestService()

	sent, balance, err := service.Send(context.Background(), validSend())
	if err != nil {
		t.Fatal(err)
	}

	record := store.sent[0]
	if record.Transfer.RecipientRiderID != recipientRider || record.Transfer.SenderPhone != senderPhone ||
		record.Transfer.Note != "dinner" || record.Transfer.CurrencyCode != "IQD" ||
		!record.DailyAmount.Equal(dec(200000)) || record.DailyCount != 5 {
		t.Fatalf("record %+v", record)
	}

	if sent.ID == "" || balance.ID == "" {
		t.Fatalf("sent %+v, balance %+v", sent, balance)
	}

	direction, counterpart := sent.Seen(recipientRider)
	if direction != Received || counterpart != senderPhone {
		t.Fatalf("seen by the recipient: %s %s", direction, counterpart)
	}

	if direction, counterpart := sent.Seen(senderRider); direction != Sent || counterpart != recipientPhone {
		t.Fatalf("seen by the sender: %s %s", direction, counterpart)
	}
}

func TestARetryIsTheSameTransfer(t *testing.T) {
	service, store, identity := newTestService()

	first, _, err := service.Send(context.Background(), validSend())
	if err != nil {
		t.Fatal(err)
	}

	again, balance, err := service.Send(context.Background(), validSend())
	if err != nil || again.ID != first.ID || balance.ID != "" || len(store.sent) != 1 || identity.pinCalls != 1 {
		t.Fatalf("retry: %+v %v, sends %d, PIN checks %d", again, err, len(store.sent), identity.pinCalls)
	}

	other := validSend()
	other.Amount = dec(6000)

	if _, _, err := service.Send(context.Background(), other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("another amount with the key: %v", err)
	}
}

func TestTheSendIsChecked(t *testing.T) {
	cases := []struct {
		name   string
		change func(*SendInput, *fakeIdentity, *fakeStore)
		want   error
	}{
		{"no key", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.IdempotencyKey = " " }, ErrIdempotencyKeyNeeded},
		{"no person", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.SenderIdentityID = "" }, ErrIdentityRequired},
		{"zero", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.Amount = dec(0) }, ErrInvalidAmount},
		{"fils past three decimals", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) {
			in.Amount = decimal.RequireFromString("1000.0001")
		}, ErrInvalidAmount},
		{"a long note", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) {
			in.Note = string(make([]rune, 141))
		}, ErrNoteTooLong},
		{"a local phone", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.RecipientPhone = "07701234567" }, ErrInvalidPhone},
		{"too little", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.Amount = dec(100) }, ErrBelowMin},
		{"too much", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.Amount = dec(100001) }, ErrAboveMax},
		{"no PIN yet", func(_ *SendInput, id *fakeIdentity, _ *fakeStore) { id.pin = PINResult{Check: PINNotSet} }, ErrPINNotSet},
		{"nobody with that phone", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.RecipientPhone = "+9647700000000" }, ErrRecipientNotFound},
		{"a person who is no rider", func(_ *SendInput, id *fakeIdentity, _ *fakeStore) {
			id.phones[recipientPhone] = "identity-driver-only"
		}, ErrRecipientNotFound},
		{"to themselves", func(in *SendInput, _ *fakeIdentity, _ *fakeStore) { in.RecipientPhone = senderPhone }, ErrToSelf},
		{"the day's limit", func(_ *SendInput, _ *fakeIdentity, s *fakeStore) { s.sendErr = ErrDailyLimit }, ErrDailyLimit},
		{"not enough money", func(_ *SendInput, _ *fakeIdentity, s *fakeStore) { s.sendErr = wallet.ErrInsufficientFunds }, wallet.ErrInsufficientFunds},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, store, identity := newTestService()
			input := validSend()
			tc.change(&input, identity, store)

			if _, _, err := service.Send(context.Background(), input); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAWrongOrLockedPINMovesNothing(t *testing.T) {
	service, store, identity := newTestService()

	identity.pin = PINResult{Check: PINWrong, AttemptsLeft: 2}

	var wrong *WrongPINError
	if _, _, err := service.Send(context.Background(), validSend()); !errors.As(err, &wrong) || wrong.AttemptsLeft != 2 {
		t.Fatalf("wrong: %v", err)
	}

	until := time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC)
	identity.pin = PINResult{Check: PINLocked, LockedUntil: until}

	var locked *LockedPINError
	if _, _, err := service.Send(context.Background(), validSend()); !errors.As(err, &locked) || !locked.Until.Equal(until) {
		t.Fatalf("locked: %v", err)
	}

	if len(store.sent) != 0 {
		t.Fatal("nothing may move without the PIN")
	}
}

func TestThePINIsCheckedBeforeTheRecipientIsLookedUp(t *testing.T) {
	service, _, identity := newTestService()
	identity.pin = PINResult{Check: PINWrong, AttemptsLeft: 4}
	identity.lookupErr = errors.New("must not be asked")

	var wrong *WrongPINError
	if _, _, err := service.Send(context.Background(), validSend()); !errors.As(err, &wrong) {
		t.Fatalf("got %v", err)
	}
}

func TestListingIsPaged(t *testing.T) {
	service, store, _ := newTestService()
	store.list = []Transfer{{ID: "1"}, {ID: "2"}, {ID: "3"}}

	page, err := service.List(context.Background(), senderRider, 2, "")
	if err != nil || len(page.Transfers) != 2 || page.NextOffset != 2 || store.offsets != [2]int{0, 3} {
		t.Fatalf("page %+v %v %v", page, err, store.offsets)
	}

	if _, err := service.List(context.Background(), senderRider, 0, "x"); !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("bad token: %v", err)
	}
}

func TestMaskingAPhone(t *testing.T) {
	if got := MaskPhone("+9647701234567"); got != "+964770***4567" {
		t.Fatalf("got %s", got)
	}

	if got := MaskPhone(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
