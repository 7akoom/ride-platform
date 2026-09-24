package topup

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const riderID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type fakeRepo struct {
	byRef  map[string]TopUp
	marked []Status
}

func (f *fakeRepo) Create(_ context.Context, t TopUp) (TopUp, error) {
	t.ID, t.ExternalReferenceID = "topup-1", "ref-1"
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

func (f *fakeRepo) FindForOwner(context.Context, wallet.OwnerType, string, string) (TopUp, error) {
	return TopUp{}, ErrTopUpNotFound
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

func (f *fakeRepo) MarkFailed(_ context.Context, ref, _ string) (TopUp, error) {
	return f.set(ref, StatusFailed)
}

type fakeConfig struct{}

func (fakeConfig) GetActiveConfig(context.Context) (wallet.Config, error) {
	return wallet.Config{CurrencyCode: "IQD", TopUpMinAmount: decimal.NewFromInt(1000), TopUpMaxAmount: decimal.NewFromInt(100000)}, nil
}

type fakeProvider struct {
	started StartInput
	notice  Notice
	err     error
}

func (p *fakeProvider) Start(_ context.Context, in StartInput) (StartResult, error) {
	p.started = in

	return StartResult{ProviderTransactionID: "p-1", RedirectURL: "https://pay.example/p-1"}, p.err
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

func newTestService() (Service, *fakeRepo, *fakeProvider, *fakeWallets) {
	repo := &fakeRepo{byRef: map[string]TopUp{}}
	provider := &fakeProvider{}
	wallets := &fakeWallets{}

	return NewService(repo, fakeConfig{}, map[string]Provider{ProviderZainCash: provider}, wallets, "ok", "ko"), repo, provider, wallets
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

	created, url, err := s.Initiate(ctx, InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000), Provider: " ZainCash "})
	if err != nil || url != "https://pay.example/p-1" || created.Provider != ProviderZainCash || created.ProviderTransactionID != "p-1" {
		t.Fatalf("initiate %+v %q %v", created, url, err)
	}

	if provider.started.ReferenceID != "ref-1" || provider.started.OwnerType != wallet.OwnerRider || provider.started.CurrencyCode != "IQD" {
		t.Fatalf("started %+v", provider.started)
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

	if _, _, err := s.Initiate(ctx, InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000)}); err != nil {
		t.Fatal(err)
	}

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

func TestACreditMadeBeforeACrashStillClosesTheTopUp(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	ctx := context.Background()

	if _, _, err := s.Initiate(ctx, InitiateInput{OwnerType: wallet.OwnerDriver, OwnerID: riderID, Amount: decimal.NewFromInt(5000)}); err != nil {
		t.Fatal(err)
	}

	// The wallet already has this credit (the record was not marked the
	// first time): the retry closes the record, and nothing moves twice.
	wallets.err = wallet.ErrDuplicateRequest
	provider.notice = Notice{ReferenceID: "ref-1", Outcome: OutcomeSucceeded}

	if err := s.ProcessNotice(ctx, ProviderZainCash, "token"); err != nil || repo.byRef["ref-1"].Status != StatusSucceeded {
		t.Fatalf("got %v %+v", err, repo.byRef["ref-1"])
	}
}

func TestAFailureIsRecorded(t *testing.T) {
	s, repo, provider, wallets := newTestService()
	ctx := context.Background()

	if _, _, err := s.Initiate(ctx, InitiateInput{OwnerType: wallet.OwnerRider, OwnerID: riderID, Amount: decimal.NewFromInt(5000)}); err != nil {
		t.Fatal(err)
	}

	provider.notice = Notice{ReferenceID: "ref-1", Outcome: OutcomeFailed}

	if err := s.ProcessNotice(ctx, ProviderZainCash, "token"); err != nil || repo.byRef["ref-1"].Status != StatusFailed || len(wallets.credits) != 0 {
		t.Fatalf("got %v %+v", err, repo.byRef["ref-1"])
	}
}
