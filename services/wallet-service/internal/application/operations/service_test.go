package operations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	staffID  = "5555aaaa-5555-4555-8555-555555555555"
	riderID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	driverID = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	tripID   = "11111111-1111-4111-8111-111111111111"
	payoutID = "99999999-9999-4999-8999-999999999999"
)

func dec(v int64) decimal.Decimal { return decimal.NewFromInt(v) }

type fakeStore struct {
	Store

	adjustments map[string]Adjustment
	adjusts     int
	refunds     int
	payouts     map[string]Payout
	requested   int
	setTo       PayoutStatus
	reference   string
	rejectedBy  string
}

func newFakeStore() *fakeStore {
	return &fakeStore{adjustments: map[string]Adjustment{}, payouts: map[string]Payout{}}
}

func (f *fakeStore) Config(context.Context) (wallet.Config, error) {
	return wallet.Config{CurrencyCode: "IQD", MinimumPayoutAmount: dec(10000)}, nil
}

func (f *fakeStore) FindAdjustmentByKey(_ context.Context, by, key string) (Adjustment, bool, error) {
	a, ok := f.adjustments[by+"/"+key]

	return a, ok, nil
}

func (f *fakeStore) Adjust(_ context.Context, r AdjustRecord) (Adjustment, wallet.Wallet, error) {
	f.adjusts++
	a := Adjustment{ID: "adj-1", Kind: KindAdjustment, OwnerType: r.OwnerType, OwnerID: r.OwnerID, Amount: r.Amount, Reason: r.Reason}
	f.adjustments[r.CreatedBy+"/"+r.IdempotencyKey] = a

	return a, wallet.Wallet{ID: "w1"}, nil
}

func (f *fakeStore) Refund(_ context.Context, r RefundRecord) (Adjustment, wallet.Wallet, error) {
	f.refunds++
	a := Adjustment{ID: "ref-1", Kind: KindRefund, OwnerType: wallet.OwnerRider, OwnerID: riderID, TripID: r.TripID, Amount: r.Amount, DriverAmount: r.DriverAmount}
	f.adjustments[r.CreatedBy+"/"+r.IdempotencyKey] = a

	return a, wallet.Wallet{ID: "w1"}, nil
}

func (f *fakeStore) FindPayoutByKey(_ context.Context, driver, key string) (Payout, bool, error) {
	p, ok := f.payouts[driver+"/"+key]

	return p, ok, nil
}

func (f *fakeStore) RequestPayout(_ context.Context, r PayoutRecord) (Payout, wallet.Wallet, wallet.Transaction, error) {
	f.requested++
	p := Payout{ID: payoutID, DriverID: r.DriverID, Amount: r.Amount, Destination: r.Destination, Status: PayoutPending}
	f.payouts[r.DriverID+"/"+r.IdempotencyKey] = p

	return p, wallet.Wallet{ID: "w2"}, wallet.Transaction{ID: "t1"}, nil
}

func (f *fakeStore) ListPayouts(context.Context, string, PayoutStatus, int, int) ([]Payout, error) {
	return []Payout{{ID: "a"}, {ID: "b"}, {ID: "c"}}, nil
}

func (f *fakeStore) SetPayoutStatus(_ context.Context, id string, to PayoutStatus, _, reference string, _ time.Time) (Payout, error) {
	f.setTo, f.reference = to, reference

	return Payout{ID: id, Status: to}, nil
}

func (f *fakeStore) RejectPayout(_ context.Context, id, by, _ string, _ time.Time) (Payout, error) {
	f.rejectedBy = by

	return Payout{ID: id, Status: PayoutRejected}, nil
}

func newTestService(store *fakeStore) *Service {
	return NewService(store, fakeWallets{})
}

type fakeWallets struct{ wallet.Service }

func validAdjust() AdjustInput {
	return AdjustInput{StaffIdentityID: staffID, OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: dec(-500), Reason: "duplicate top-up", IdempotencyKey: "k1"}
}

func TestAnAdjustmentIsCheckedAndMadeOnce(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(*AdjustInput)
		want error
	}{
		"no staff":       {func(in *AdjustInput) { in.StaffIdentityID = "" }, ErrStaffRequired},
		"no owner type":  {func(in *AdjustInput) { in.OwnerType = "" }, ErrOwnerRequired},
		"an owner id":    {func(in *AdjustInput) { in.OwnerID = "rider-1" }, ErrOwnerRequired},
		"zero":           {func(in *AdjustInput) { in.Amount = decimal.Zero }, ErrInvalidAmount},
		"four decimals":  {func(in *AdjustInput) { in.Amount = decimal.RequireFromString("0.0001") }, ErrInvalidAmount},
		"a short reason": {func(in *AdjustInput) { in.Reason = "no" }, ErrReasonRequired},
		"a long reason":  {func(in *AdjustInput) { in.Reason = strings.Repeat("x", 301) }, ErrReasonRequired},
		"no key":         {func(in *AdjustInput) { in.IdempotencyKey = " " }, ErrIdempotencyKey},
	} {
		input := validAdjust()
		c.edit(&input)

		if _, _, err := newTestService(newFakeStore()).Adjust(context.Background(), input); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	store := newFakeStore()
	s := newTestService(store)

	if _, balance, err := s.Adjust(context.Background(), validAdjust()); err != nil || balance.ID != "w1" {
		t.Fatalf("adjust %+v %v", balance, err)
	}

	// The same key again: the same adjustment, nothing moves, the wallet is
	// left for the caller to read.
	if again, balance, err := s.Adjust(context.Background(), validAdjust()); err != nil || again.ID != "adj-1" || balance.ID != "" || store.adjusts != 1 {
		t.Fatalf("again %+v %+v %v (adjusts %d)", again, balance, err, store.adjusts)
	}

	other := validAdjust()
	other.Amount = dec(-600)

	if _, _, err := s.Adjust(context.Background(), other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key for another amount: %v", err)
	}
}

func TestARefundIsCheckedAndMadeOnce(t *testing.T) {
	valid := RefundInput{StaffIdentityID: staffID, TripID: tripID, Amount: dec(2000), DriverAmount: dec(500), Reason: "wrong route", IdempotencyKey: "r1"}

	for name, c := range map[string]struct {
		edit func(*RefundInput)
		want error
	}{
		"no trip":                     {func(in *RefundInput) { in.TripID = "trip-1" }, ErrTripRequired},
		"a negative amount":           {func(in *RefundInput) { in.Amount = dec(-1) }, ErrInvalidRefund},
		"the driver giving back more": {func(in *RefundInput) { in.DriverAmount = dec(2001) }, ErrInvalidRefund},
		"a negative driver amount":    {func(in *RefundInput) { in.DriverAmount = dec(-1) }, ErrInvalidRefund},
		"no reason":                   {func(in *RefundInput) { in.Reason = "" }, ErrReasonRequired},
	} {
		input := valid
		c.edit(&input)

		if _, _, err := newTestService(newFakeStore()).Refund(context.Background(), input); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	store := newFakeStore()
	s := newTestService(store)

	if _, _, err := s.Refund(context.Background(), valid); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.Refund(context.Background(), valid); err != nil || store.refunds != 1 {
		t.Fatalf("again: %v (refunds %d)", err, store.refunds)
	}

	other := valid
	other.DriverAmount = dec(0)

	if _, _, err := s.Refund(context.Background(), other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key for another refund: %v", err)
	}
}

func TestAPayoutRequestIsCheckedAndMadeOnce(t *testing.T) {
	store := newFakeStore()
	s := newTestService(store)
	ctx := context.Background()

	valid := PayoutInput{DriverID: driverID, Amount: dec(15000), Destination: "ZainCash +9647500000009", IdempotencyKey: "p1"}

	for name, c := range map[string]struct {
		edit func(*PayoutInput)
		want error
	}{
		"no driver":          {func(in *PayoutInput) { in.DriverID = "" }, ErrDriverRequired},
		"zero":               {func(in *PayoutInput) { in.Amount = decimal.Zero }, wallet.ErrInvalidAmount},
		"below the minimum":  {func(in *PayoutInput) { in.Amount = dec(9999) }, wallet.ErrBelowMinimumPayout},
		"a long destination": {func(in *PayoutInput) { in.Destination = strings.Repeat("x", 121) }, ErrDestinationTooLong},
		"no idempotency key": {func(in *PayoutInput) { in.IdempotencyKey = "" }, ErrIdempotencyKey},
	} {
		input := valid
		c.edit(&input)

		if _, _, _, err := s.RequestPayout(ctx, input); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	if _, _, hold, err := s.RequestPayout(ctx, valid); err != nil || hold.ID != "t1" {
		t.Fatalf("request %+v %v", hold, err)
	}

	if again, balance, _, err := s.RequestPayout(ctx, valid); err != nil || again.ID != payoutID || balance.ID != "" || store.requested != 1 {
		t.Fatalf("again %+v %v (requested %d)", again, err, store.requested)
	}

	other := valid
	other.Amount = dec(20000)

	if _, _, _, err := s.RequestPayout(ctx, other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key for another amount: %v", err)
	}
}

func TestThePayoutQueueIsWorkedByStaff(t *testing.T) {
	store := newFakeStore()
	s := newTestService(store)
	ctx := context.Background()

	if p, err := s.ApprovePayout(ctx, staffID, payoutID); err != nil || p.Status != PayoutApproved {
		t.Fatalf("approve %+v %v", p, err)
	}

	if _, err := s.MarkPayoutPaid(ctx, staffID, payoutID, " "); !errors.Is(err, ErrReferenceRequired) {
		t.Fatalf("paid without a reference: %v", err)
	}

	if _, err := s.MarkPayoutPaid(ctx, staffID, payoutID, " ZC-1 "); err != nil || store.setTo != PayoutPaid || store.reference != "ZC-1" {
		t.Fatalf("paid: %v %q", err, store.reference)
	}

	if _, err := s.RejectPayout(ctx, staffID, payoutID, "x"); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("reject without a reason: %v", err)
	}

	if _, err := s.RejectPayout(ctx, "", payoutID, "wrong number"); !errors.Is(err, ErrStaffRequired) {
		t.Fatalf("reject by nobody: %v", err)
	}

	if _, err := s.ApprovePayout(ctx, staffID, "p-1"); !errors.Is(err, ErrPayoutNotFound) {
		t.Fatalf("a bad id: %v", err)
	}

	page, err := s.ListPayouts(ctx, "", "pending", 2, "")
	if err != nil || len(page.Payouts) != 2 || page.NextOffset != 2 {
		t.Fatalf("page %+v %v", page, err)
	}

	if _, err := s.ListPayouts(ctx, "", "sent", 0, ""); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("status: %v", err)
	}
}
