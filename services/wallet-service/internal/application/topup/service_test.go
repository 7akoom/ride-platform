package topup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	riderID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	topUpID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

var start = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

type fakeRepo struct {
	byRef  map[string]TopUp
	marked []Status
	reason string
}

func (f *fakeRepo) Create(_ context.Context, t TopUp) (TopUp, error) {
	t.ID, t.ExternalReferenceID, t.CreatedAt = topUpID, "ref-1", start
	f.byRef[t.ExternalReferenceID] = t

	return t, nil
}

func (f *fakeRepo) FindByExternalReferenceID(_ context.Context, ref string) (TopUp, error) {
	t, ok := f.byRef[ref]
	if !ok {
		return TopUp{}, ErrTopUpNotFound
	}

	return t, nil
}

func (f *fakeRepo) FindForOwner(_ context.Context, ownerType wallet.OwnerType, ownerID, id string) (TopUp, error) {
	for _, t := range f.byRef {
		if t.ID == id && t.OwnerType == ownerType && t.OwnerID == ownerID {
			return t, nil
		}
	}

	return TopUp{}, ErrTopUpNotFound
}

func (f *fakeRepo) ListPending(_ context.Context, since, before time.Time, limit int) ([]TopUp, error) {
	var pending []TopUp

	for _, t := range f.byRef {
		if t.Status == StatusPending && !t.CreatedAt.Before(since) && t.CreatedAt.Before(before) && len(pending) < limit {
			pending = append(pending, t)
		}
	}

	return pending, nil
}

func (f *fakeRepo) SetProviderTransactionID(_ context.Context, ref, id string) (TopUp, error) {
	t := f.byRef[ref]
	t.ProviderTransactionID = id
	f.byRef[ref] = t

	return t, nil
}

func (f *fakeRepo) set(ref string, s Status) (TopUp, error) {
	t := f.byRef[ref]
	t.Status = s
	f.byRef[ref] = t
	f.marked = append(f.marked, s)

	return t, nil
}

func (f *fakeRepo) MarkSucceeded(_ context.Context, ref string) (TopUp, error) {
	return f.set(ref, StatusSucceeded)
}

func (f *fakeRepo) MarkFailed(_ context.Context, ref, reason string) (TopUp, error) {
	if f.byRef[ref].Status == StatusSucceeded {
		return TopUp{}, ErrTopUpNotFound
	}

	f.reason = reason

	return f.set(ref, StatusFailed)
}

type fakeConfig struct{}

func (fakeConfig) GetActiveConfig(context.Context) (wallet.Config, error) {
	return wallet.Config{CurrencyCode: "IQD", TopUpMinAmount: decimal.NewFromInt(1000), TopUpMaxAmount: decimal.NewFromInt(100000)}, nil
}

type fakeProvider struct {
	started  StartInput
	notice   Notice
	checked  []string
	answer   Notice
	checkErr error
	err      error
}

func (p *fakeProvider) Start(_ context.Context, in StartInput) (StartResult, error) {
	p.started = in

	return StartResult{ProviderTransactionID: "p-1", RedirectURL: "https://pay.example/p-1"}, p.err
}

func (p *fakeProvider) Check(_ context.Context, id string) (Notice, error) {
	p.checked = append(p.checked, id)

	return p.answer, p.checkErr
}

func (p *fakeProvider) Verify(string) (Notice, error) { return p.notice, nil }

type fakeWallets struct {
	wallet.Service
	credits []wallet.TopUpInput
	err     error
}

func (w *fakeWallets) TopUp(_ context.Context, in wallet.TopUpInput) (wallet.Wallet, wallet.Transaction, error) {
	w.credits = append(w.credits, in)

	return wallet.Wallet{}, wallet.Transaction{}, w.err
}

func newTestService() (*service, *fakeRepo, *fakeProvider, *fakeWallets) {
	repo := &fakeRepo{byRef: map[string]TopUp{}}
	provider := &fakeProvider{}
	wallets := &fakeWallets{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := NewService(repo, fakeConfig{}, map[string]Provider{ProviderZainCash: provider}, wallets, "ok", "ko", logger).(*service)
	s.now = func() time.Time { return start }

	return s, repo, provider, wallets
}

// opened is a rider's top-up of 5000, open at the provider as p-1.
func opened(t *testing.T, s *service) {
	t.Helper()

	if _, _, err := s.Initiate(context.Background(), InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000)}); err != nil {
		t.Fatal(err)
	}
}

func TestATopUpIsCheckedBeforeThePaymentStarts(t *testing.T) {
	s, _, provider, _ := newTestService()
	ctx := context.Background()

	for name, c := range map[string]struct {
		input InitiateInput
		want  error
	}{
		"no owner":        {InitiateInput{OwnerType: wallet.OwnerRider, Amount: decimal.NewFromInt(5000)}, ErrOwnerRequired},
		"no owner type":   {InitiateInput{OwnerID: riderID, Amount: decimal.NewFromInt(5000)}, ErrOwnerRequired},
		"zero":            {InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID}, ErrInvalidAmount},
		"below the least": {InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(999)}, ErrBelowMinimum},
		"above the most":  {InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(100001)}, ErrAboveMaximum},
		"an unknown one":  {InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000), Provider: "paypal"}, ErrUnknownProvider},
	} {
		if _, _, err := s.Initiate(ctx, c.input); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	created, url, err := s.Initiate(ctx, InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000), Provider: " ZainCash ", Language: "ku"})
	if err != nil || url != "https://pay.example/p-1" || created.Provider != ProviderZainCash || created.ProviderTransactionID != "p-1" {
		t.Fatalf("initiate %+v %q %v", created, url, err)
	}

	if provider.started.ReferenceID != "ref-1" || provider.started.OwnerType != wallet.OwnerRider ||
		provider.started.CurrencyCode != "IQD" || provider.started.Language != "ku" || provider.started.SuccessURL != "ok" {
		t.Fatalf("started %+v", provider.started)
	}
}

func TestWithoutAProviderTopUpsAreOff(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(&fakeRepo{byRef: map[string]TopUp{}}, fakeConfig{}, nil, &fakeWallets{}, "ok", "ko", logger)

	if _, _, err := s.Initiate(context.Background(), InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000)}); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("initiate: %v", err)
	}

	options, err := s.Options(context.Background())
	if err != nil || len(options.Providers) != 0 || !options.MinAmount.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("options %+v %v", options, err)
	}
}

func TestTheOptionsAreTheLimitsAndTheProviders(t *testing.T) {
	s, _, _, _ := newTestService()

	options, err := s.Options(context.Background())
	if err != nil || options.CurrencyCode != "IQD" || !options.MaxAmount.Equal(decimal.NewFromInt(100000)) ||
		len(options.Providers) != 1 || options.Providers[0] != ProviderZainCash {
		t.Fatalf("options %+v %v", options, err)
	}
}

func TestAProviderRefusingTheAmountLeavesAFailedRecord(t *testing.T) {
	s, repo, provider, _ := newTestService()
	provider.err = ErrAmountNotSupported

	if _, _, err := s.Initiate(context.Background(), InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.RequireFromString("1500.5")}); !errors.Is(err, ErrAmountNotSupported) {
		t.Fatalf("got %v", err)
	}

	if repo.byRef["ref-1"].Status != StatusFailed {
		t.Fatalf("record %+v", repo.byRef["ref-1"])
	}
}

func TestASuccessCreditsTheWalletOnce(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	ctx := context.Background()
	opened(t, s)

	provider.notice = Notice{ReferenceID: "ref-1", Outcome: OutcomePending}
	if err := s.ProcessNotice(ctx, ProviderZainCash, "token"); err != nil || len(wallets.credits) != 0 {
		t.Fatalf("pending: %v %+v", err, wallets.credits)
	}

	provider.notice = Notice{ReferenceID: "ref-1", ProviderTransactionID: "p-1", Outcome: OutcomeSucceeded}
	if err := s.ProcessNotice(ctx, ProviderZainCash, "token"); err != nil {
		t.Fatal(err)
	}

	credit := wallets.credits[0]
	if credit.OwnerType != wallet.OwnerRider || credit.OwnerID != riderID || credit.IdempotencyKey != "zaincash:p-1" || !credit.Amount.Equal(decimal.NewFromInt(5000)) {
		t.Fatalf("credit %+v", credit)
	}

	// Delivered again: resolved already, nothing more.
	if err := s.ProcessNotice(ctx, ProviderZainCash, "token"); err != nil || len(wallets.credits) != 1 {
		t.Fatalf("again: %v %d", err, len(wallets.credits))
	}

	if len(repo.marked) != 1 || repo.marked[0] != StatusSucceeded {
		t.Fatalf("marked %+v", repo.marked)
	}

	if err := s.ProcessNotice(ctx, "paypal", "token"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("an unknown provider: %v", err)
	}
}

func TestANoticeNamingAnotherTransactionIsRefused(t *testing.T) {
	s, _, provider, wallets := newTestService()
	opened(t, s)

	provider.notice = Notice{ReferenceID: "ref-1", ProviderTransactionID: "p-2", Outcome: OutcomeSucceeded}
	if err := s.ProcessNotice(context.Background(), ProviderZainCash, "token"); !errors.Is(err, ErrInvalidWebhookToken) || len(wallets.credits) != 0 {
		t.Fatalf("got %v %+v", err, wallets.credits)
	}
}

func TestACreditMadeBeforeACrashStillClosesTheTopUp(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	opened(t, s)

	// The wallet already has this credit (the record was not marked the
	// first time): the retry closes the record, and nothing moves twice.
	wallets.err = wallet.ErrDuplicateRequest
	provider.notice = Notice{ReferenceID: "ref-1", Outcome: OutcomeSucceeded}

	if err := s.ProcessNotice(context.Background(), ProviderZainCash, "token"); err != nil || repo.byRef["ref-1"].Status != StatusSucceeded {
		t.Fatalf("got %v %+v", err, repo.byRef["ref-1"])
	}
}

func TestAFailureIsRecordedWithTheProvidersWord(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	opened(t, s)

	provider.notice = Notice{ReferenceID: "ref-1", Outcome: OutcomeFailed, Detail: "zaincash: EXPIRED"}

	if err := s.ProcessNotice(context.Background(), ProviderZainCash, "token"); err != nil || repo.byRef["ref-1"].Status != StatusFailed || len(wallets.credits) != 0 {
		t.Fatalf("got %v %+v", err, repo.byRef["ref-1"])
	}

	if repo.reason != "zaincash: EXPIRED" {
		t.Fatalf("reason %q", repo.reason)
	}
}

func TestTheAppAskingSettlesAPaidTopUp(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	ctx := context.Background()
	opened(t, s)

	provider.answer = Notice{ReferenceID: "ref-1", ProviderTransactionID: "p-1", Outcome: OutcomePending}

	got, err := s.Get(ctx, wallet.OwnerRider, riderID, topUpID)
	if err != nil || got.Status != StatusPending || len(provider.checked) != 1 || provider.checked[0] != "p-1" {
		t.Fatalf("still paying: %+v %v %v", got, err, provider.checked)
	}

	provider.answer.Outcome = OutcomeSucceeded

	got, err = s.Get(ctx, wallet.OwnerRider, riderID, topUpID)
	if err != nil || got.Status != StatusSucceeded || len(wallets.credits) != 1 || wallets.credits[0].IdempotencyKey != "zaincash:p-1" {
		t.Fatalf("paid: %+v %v %+v", got, err, wallets.credits)
	}

	// Closed now: nobody asks the provider again.
	if _, err := s.Get(ctx, wallet.OwnerRider, riderID, topUpID); err != nil || len(provider.checked) != 2 || len(repo.marked) != 1 {
		t.Fatalf("closed: %v %v %v", err, provider.checked, repo.marked)
	}
}

func TestTheProviderNotAnsweringLeavesTheTopUpPending(t *testing.T) {
	s, _, provider, wallets := newTestService()
	opened(t, s)

	provider.checkErr = errors.New("zaincash: 503")

	got, err := s.Get(context.Background(), wallet.OwnerRider, riderID, topUpID)
	if err != nil || got.Status != StatusPending || len(wallets.credits) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestAnAnswerAboutAnotherPaymentIsNotApplied(t *testing.T) {
	s, _, provider, wallets := newTestService()
	opened(t, s)

	provider.answer = Notice{ReferenceID: "ref-2", Outcome: OutcomeSucceeded}

	got, err := s.Get(context.Background(), wallet.OwnerRider, riderID, topUpID)
	if err != nil || got.Status != StatusPending || len(wallets.credits) != 0 {
		t.Fatalf("got %+v %v %+v", got, err, wallets.credits)
	}
}

func TestTheSweepSettlesWhatEndedAndClosesWhatNeverOpened(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	ctx := context.Background()
	opened(t, s)

	// A second top-up whose payment never opened at the provider.
	repo.byRef["ref-unopened"] = TopUp{ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", OwnerType: wallet.OwnerRider, OwnerID: riderID,
		Provider: ProviderZainCash, ExternalReferenceID: "ref-unopened", Status: StatusPending, CreatedAt: start}

	// The customer may still be paying: nothing is asked yet.
	if settled, err := s.SettlePending(ctx); err != nil || settled != 0 || len(provider.checked) != 0 {
		t.Fatalf("too soon: %d %v %v", settled, err, provider.checked)
	}

	s.now = func() time.Time { return start.Add(20 * time.Minute) }
	provider.answer = Notice{ReferenceID: "ref-1", ProviderTransactionID: "p-1", Outcome: OutcomeSucceeded}

	settled, err := s.SettlePending(ctx)
	if err != nil || settled != 2 || len(wallets.credits) != 1 {
		t.Fatalf("swept: %d %v %+v", settled, err, wallets.credits)
	}

	if repo.byRef["ref-1"].Status != StatusSucceeded || repo.byRef["ref-unopened"].Status != StatusFailed {
		t.Fatalf("records %+v", repo.byRef)
	}

	// Nothing left to do.
	if settled, err := s.SettlePending(ctx); err != nil || settled != 0 || len(provider.checked) != 1 {
		t.Fatalf("again: %d %v %v", settled, err, provider.checked)
	}

	// Older than the sweep's window: left for staff.
	s.now = func() time.Time { return start.Add(sweepWindow + time.Hour) }
	repo.byRef["ref-1"] = TopUp{ID: topUpID, OwnerType: wallet.OwnerRider, OwnerID: riderID, Provider: ProviderZainCash,
		ExternalReferenceID: "ref-1", ProviderTransactionID: "p-1", Status: StatusPending, CreatedAt: start}

	if settled, err := s.SettlePending(ctx); err != nil || settled != 0 || len(provider.checked) != 1 {
		t.Fatalf("too old: %d %v %v", settled, err, provider.checked)
	}
}

func TestAFailureNeverUndoesASuccess(t *testing.T) {
	s, repo, _, _ := newTestService()
	opened(t, s)

	record := repo.byRef["ref-1"]
	repo.byRef["ref-1"] = TopUp{ID: record.ID, OwnerType: record.OwnerType, OwnerID: record.OwnerID, Provider: record.Provider,
		ExternalReferenceID: "ref-1", ProviderTransactionID: "p-1", Status: StatusSucceeded, CreatedAt: start}

	// A stale copy still pending meets a failure: the success stands.
	got, err := s.settle(context.Background(), record, Notice{Outcome: OutcomeFailed})
	if err != nil || got.Status != StatusSucceeded {
		t.Fatalf("got %+v %v", got, err)
	}
}
