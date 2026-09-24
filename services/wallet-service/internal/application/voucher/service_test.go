package voucher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var testNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	byKey      map[string]Batch
	created    []Sealed
	createErrs []error

	redeemErr  error
	redeemed   Redemption
	redeems    int
	failures   []time.Time
	recordErr  error
	exported   []Exported
	exportErr  error
	exportOpen func(hash, sealed []byte) (string, error)
}

func newFakeStore() *fakeStore { return &fakeStore{byKey: map[string]Batch{}} }

func (f *fakeStore) Config(context.Context) (wallet.Config, error) {
	return wallet.Config{CurrencyCode: "IQD"}, nil
}

func (f *fakeStore) CreateBatch(_ context.Context, b Batch, codes []Sealed) (Batch, error) {
	if len(f.createErrs) > 0 {
		err := f.createErrs[0]
		f.createErrs = f.createErrs[1:]

		if err != nil {
			return Batch{}, err
		}
	}

	f.created = codes
	b.ID = "batch-1"
	b.Number = 7
	f.byKey[b.CreatedBy+"/"+b.IdempotencyKey] = b

	return b, nil
}

func (f *fakeStore) FindBatchByKey(_ context.Context, by, key string) (Batch, bool, error) {
	b, ok := f.byKey[by+"/"+key]

	return b, ok, nil
}

func (f *fakeStore) GetBatch(context.Context, string) (Batch, error) {
	return Batch{}, ErrBatchNotFound
}

func (f *fakeStore) ListBatches(context.Context, BatchStatus, int, int) ([]Batch, error) {
	return []Batch{{ID: "a"}, {ID: "b"}, {ID: "c"}}, nil
}

func (f *fakeStore) ExportBatch(_ context.Context, _, _ string, _ time.Time, open func(hash, sealed []byte) (string, error)) (Batch, []Exported, error) {
	f.exportOpen = open

	return Batch{ID: "batch-1", Amount: decimal.NewFromInt(5000), CurrencyCode: "IQD", ExpiresAt: testNow}, f.exported, f.exportErr
}

func (f *fakeStore) CancelBatch(context.Context, string, string, string, time.Time) (Batch, error) {
	return Batch{Status: BatchCancelled}, nil
}

func (f *fakeStore) GetVoucher(_ context.Context, serial string) (Voucher, error) {
	return Voucher{Serial: serial}, nil
}

func (f *fakeStore) VoidVoucher(_ context.Context, serial, _, _ string, _ time.Time) (Voucher, error) {
	return Voucher{Serial: serial, Status: Void}, nil
}

func (f *fakeStore) Redeem(context.Context, string, []byte, time.Time) (Redemption, error) {
	f.redeems++

	return f.redeemed, f.redeemErr
}

func (f *fakeStore) Failures(_ context.Context, _ string, since time.Time) (int, time.Time, error) {
	count, oldest := 0, time.Time{}

	for _, at := range f.failures {
		if at.After(since) {
			if count == 0 || at.Before(oldest) {
				oldest = at
			}

			count++
		}
	}

	return count, oldest, nil
}

func (f *fakeStore) RecordFailure(_ context.Context, _ string, at, _ time.Time) error {
	if f.recordErr != nil {
		return f.recordErr
	}

	f.failures = append(f.failures, at)

	return nil
}

func newTestService(t *testing.T, store *fakeStore) *Service {
	t.Helper()

	codec, err := NewCodec("unit-test-key")
	if err != nil {
		t.Fatal(err)
	}

	s := NewService(store, codec, Limits{MaxFailures: 3, Window: time.Hour})
	s.now = func() time.Time { return testNow }

	return s
}

func validCreate() CreateInput {
	return CreateInput{
		StaffIdentityID: "staff-1", Label: "ZainCash October", Seller: "zaincash",
		Amount: decimal.NewFromInt(5000), Quantity: 4, IdempotencyKey: "k1",
	}
}

func TestABatchIsCheckedBeforeItIsIssued(t *testing.T) {
	for name, change := range map[string]struct {
		edit func(*CreateInput)
		want error
	}{
		"no staff":           {func(in *CreateInput) { in.StaffIdentityID = " " }, ErrStaffRequired},
		"no label":           {func(in *CreateInput) { in.Label = "" }, ErrLabelRequired},
		"a long label":       {func(in *CreateInput) { in.Label = strings.Repeat("x", 121) }, ErrLabelRequired},
		"a long seller":      {func(in *CreateInput) { in.Seller = strings.Repeat("x", 61) }, ErrSellerTooLong},
		"a zero amount":      {func(in *CreateInput) { in.Amount = decimal.Zero }, ErrInvalidAmount},
		"four decimals":      {func(in *CreateInput) { in.Amount = decimal.RequireFromString("1.0001") }, ErrInvalidAmount},
		"no vouchers":        {func(in *CreateInput) { in.Quantity = 0 }, ErrInvalidQuantity},
		"too many":           {func(in *CreateInput) { in.Quantity = 10001 }, ErrInvalidQuantity},
		"ending too soon":    {func(in *CreateInput) { in.ExpiresAt = testNow.Add(30 * time.Minute) }, ErrInvalidExpiry},
		"ending too late":    {func(in *CreateInput) { in.ExpiresAt = testNow.Add(4 * 365 * 24 * time.Hour) }, ErrInvalidExpiry},
		"no key":             {func(in *CreateInput) { in.IdempotencyKey = "" }, ErrIdempotencyKey},
		"a key too long":     {func(in *CreateInput) { in.IdempotencyKey = strings.Repeat("k", 121) }, ErrIdempotencyKey},
		"ending in the past": {func(in *CreateInput) { in.ExpiresAt = testNow.Add(-time.Hour) }, ErrInvalidExpiry},
	} {
		input := validCreate()
		change.edit(&input)

		if _, err := newTestService(t, newFakeStore()).CreateBatch(context.Background(), input); !errors.Is(err, change.want) {
			t.Errorf("%s: got %v, want %v", name, err, change.want)
		}
	}
}

func TestABatchGetsDistinctSealedCodesAndAYearByDefault(t *testing.T) {
	store := newFakeStore()
	s := newTestService(t, store)

	created, err := s.CreateBatch(context.Background(), validCreate())
	if err != nil {
		t.Fatal(err)
	}

	if created.CurrencyCode != "IQD" || !created.ExpiresAt.Equal(testNow.Add(defaultValidity)) || created.Status != BatchCreated {
		t.Fatalf("created %+v", created)
	}

	if len(store.created) != 4 {
		t.Fatalf("codes %d", len(store.created))
	}

	hashes := map[string]bool{}

	for _, c := range store.created {
		code, err := s.codec.Open(c.Hash, c.Sealed)
		if err != nil {
			t.Fatal(err)
		}

		if hashes[code] {
			t.Fatal("a code twice in a batch")
		}

		hashes[code] = true
	}

	// A retry with the same key is the same batch; with another batch, refused.
	again, err := s.CreateBatch(context.Background(), validCreate())
	if err != nil || again.ID != created.ID {
		t.Fatalf("again %+v %v", again, err)
	}

	other := validCreate()
	other.Quantity = 5

	if _, err := s.CreateBatch(context.Background(), other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key for another batch: %v", err)
	}
}

func TestACodeClashIsIssuedAgainOnce(t *testing.T) {
	store := newFakeStore()
	store.createErrs = []error{ErrCodeTaken, nil}

	if _, err := newTestService(t, store).CreateBatch(context.Background(), validCreate()); err != nil {
		t.Fatalf("one clash: %v", err)
	}

	store = newFakeStore()
	store.createErrs = []error{ErrCodeTaken, ErrCodeTaken}

	if _, err := newTestService(t, store).CreateBatch(context.Background(), validCreate()); !errors.Is(err, ErrCodeTaken) {
		t.Fatalf("two clashes: %v", err)
	}
}

func TestTheExportFormatsCodesAndWritesACSV(t *testing.T) {
	store := newFakeStore()
	store.exported = []Exported{{Serial: "V7-00001", Code: "ABCDEFGHJKMNPQRS"}}
	s := newTestService(t, store)

	batch, exported, err := s.ExportBatch(context.Background(), "staff-1", "batch-1")
	if err != nil || exported[0].Code != "ABCD-EFGH-JKMN-PQRS" || store.exportOpen == nil {
		t.Fatalf("exported %+v %v", exported, err)
	}

	sheet, err := CSV(batch, exported)
	want := "serial,code,amount,currency,expires_at\nV7-00001,ABCD-EFGH-JKMN-PQRS,5000,IQD,2026-09-24T12:00:00Z\n"

	if err != nil || sheet != want {
		t.Fatalf("csv %q %v", sheet, err)
	}

	if _, _, err := s.ExportBatch(context.Background(), "", "batch-1"); !errors.Is(err, ErrStaffRequired) {
		t.Fatalf("no staff: %v", err)
	}
}

func TestCancellingAndVoidingNeedAReason(t *testing.T) {
	s := newTestService(t, newFakeStore())
	ctx := context.Background()

	if _, err := s.CancelBatch(ctx, "staff-1", "batch-1", "no"); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := s.VoidVoucher(ctx, "staff-1", "V7-00001", strings.Repeat("x", 301)); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("void: %v", err)
	}

	if v, err := s.VoidVoucher(ctx, "staff-1", " v7-00001 ", "card lost"); err != nil || v.Serial != "V7-00001" {
		t.Fatalf("void %+v %v", v, err)
	}
}

func TestWrongCodesCountUntilTheRiderWaits(t *testing.T) {
	store := newFakeStore()
	s := newTestService(t, store)
	ctx := context.Background()

	// Not a code at all: refused, not counted, the store never asked.
	if _, err := s.Redeem(ctx, "rider-1", "hello"); !errors.Is(err, ErrInvalidCode) || store.redeems != 0 || len(store.failures) != 0 {
		t.Fatalf("not a code: %v", err)
	}

	for _, err := range []error{ErrCodeNotValid, ErrCodeUsed, ErrCodeExpired} {
		store.redeemErr = err

		if _, got := s.Redeem(ctx, "rider-1", "ABCD-EFGH-JKMN-PQRS"); !errors.Is(got, err) {
			t.Fatalf("got %v, want %v", got, err)
		}
	}

	if len(store.failures) != 3 {
		t.Fatalf("failures %d", len(store.failures))
	}

	// The fourth try waits for the oldest failure to leave the window, and
	// never reaches the store.
	store.redeemErr = nil
	store.redeems = 0

	var tooMany *TooManyAttemptsError

	_, err := s.Redeem(ctx, "rider-1", "ABCD-EFGH-JKMN-PQRS")
	if !errors.As(err, &tooMany) || !tooMany.Until.Equal(testNow.Add(time.Hour)) || store.redeems != 0 {
		t.Fatalf("locked: %v", err)
	}

	// An hour later the window is clear.
	s.now = func() time.Time { return testNow.Add(time.Hour + time.Second) }

	if _, err := s.Redeem(ctx, "rider-1", "ABCD-EFGH-JKMN-PQRS"); err != nil || store.redeems != 1 {
		t.Fatalf("after the window: %v", err)
	}
}

func TestARedemptionIsNotCountedAndAFailedCountFailsClosed(t *testing.T) {
	store := newFakeStore()
	store.redeemed = Redemption{Serial: "V7-00001"}
	s := newTestService(t, store)

	if got, err := s.Redeem(context.Background(), "rider-1", "abcd efgh jkmn pqrs"); err != nil || got.Serial != "V7-00001" || len(store.failures) != 0 {
		t.Fatalf("redeemed %+v %v", got, err)
	}

	store.redeemErr = ErrCodeNotValid
	store.recordErr = errors.New("database down")

	if _, err := s.Redeem(context.Background(), "rider-1", "ABCD-EFGH-JKMN-PQRS"); err == nil || errors.Is(err, ErrCodeNotValid) {
		t.Fatalf("an uncounted failure must not look like a wrong code: %v", err)
	}

	if _, err := s.Redeem(context.Background(), "", "ABCD-EFGH-JKMN-PQRS"); !errors.Is(err, ErrRiderRequired) {
		t.Fatalf("no rider: %v", err)
	}
}

func TestBatchesArePaged(t *testing.T) {
	s := newTestService(t, newFakeStore())

	page, err := s.ListBatches(context.Background(), "", 2, "")
	if err != nil || len(page.Batches) != 2 || page.NextOffset != 2 {
		t.Fatalf("page %+v %v", page, err)
	}

	if _, err := s.ListBatches(context.Background(), "sold", 0, ""); !errors.Is(err, ErrInvalidStatusFilter) {
		t.Fatalf("status: %v", err)
	}

	if _, err := s.ListBatches(context.Background(), "", 0, "x"); !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("token: %v", err)
	}
}
