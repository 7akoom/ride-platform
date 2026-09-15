package wallet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func d(s string) wallet.Money { return decimal.RequireFromString(s) }

// --- test doubles -----------------------------------------------------

type fakeRepository struct {
	config    wallet.Config
	configErr error

	wallets map[string]wallet.Wallet // key: string(ownerType)+":"+ownerID

	applyMovementResult wallet.Wallet
	applyMovementTx     wallet.Transaction
	applyMovementErr    error
	applyMovementCalls  []wallet.MovementInput

	findSettlementResult wallet.Settlement
	findSettlementFound  bool
	findSettlementErr    error

	settleTripResult wallet.Settlement
	settleTripErr    error
	settleTripCalls  []wallet.SettleInput

	listTransactionsResult []wallet.Transaction
	listTransactionsErr    error
	listTransactionsLimit  int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{wallets: map[string]wallet.Wallet{}}
}

func walletKey(ownerType wallet.OwnerType, ownerID string) string {
	return string(ownerType) + ":" + ownerID
}

func (r *fakeRepository) GetActiveConfig(_ context.Context) (wallet.Config, error) {
	if r.configErr != nil {
		return wallet.Config{}, r.configErr
	}
	return r.config, nil
}

func (r *fakeRepository) FindOrCreateWallet(
	_ context.Context,
	ownerType wallet.OwnerType,
	ownerID string,
	currencyCode string,
) (wallet.Wallet, error) {
	if w, ok := r.wallets[walletKey(ownerType, ownerID)]; ok {
		return w, nil
	}
	return wallet.Wallet{OwnerType: ownerType, OwnerID: ownerID, CurrencyCode: currencyCode, Balance: decimal.Zero}, nil
}

func (r *fakeRepository) FindWallet(
	_ context.Context,
	ownerType wallet.OwnerType,
	ownerID string,
) (wallet.Wallet, error) {
	if w, ok := r.wallets[walletKey(ownerType, ownerID)]; ok {
		return w, nil
	}
	return wallet.Wallet{}, wallet.ErrWalletNotFound
}

func (r *fakeRepository) ApplyMovement(
	_ context.Context,
	input wallet.MovementInput,
) (wallet.Wallet, wallet.Transaction, error) {
	r.applyMovementCalls = append(r.applyMovementCalls, input)
	if r.applyMovementErr != nil {
		return wallet.Wallet{}, wallet.Transaction{}, r.applyMovementErr
	}
	return r.applyMovementResult, r.applyMovementTx, nil
}

func (r *fakeRepository) FindSettlement(
	_ context.Context,
	_ string,
) (wallet.Settlement, bool, error) {
	if r.findSettlementErr != nil {
		return wallet.Settlement{}, false, r.findSettlementErr
	}
	return r.findSettlementResult, r.findSettlementFound, nil
}

func (r *fakeRepository) SettleTrip(
	_ context.Context,
	input wallet.SettleInput,
) (wallet.Settlement, error) {
	r.settleTripCalls = append(r.settleTripCalls, input)
	if r.settleTripErr != nil {
		return wallet.Settlement{}, r.settleTripErr
	}
	return r.settleTripResult, nil
}

func (r *fakeRepository) ListTransactions(
	_ context.Context,
	_ wallet.OwnerType,
	_ string,
	limit int,
) ([]wallet.Transaction, error) {
	r.listTransactionsLimit = limit
	if r.listTransactionsErr != nil {
		return nil, r.listTransactionsErr
	}
	return r.listTransactionsResult, nil
}

func defaultConfig() wallet.Config {
	return wallet.Config{
		CurrencyCode:        "IQD",
		CommissionRate:      d("20"), // 20%
		SuspensionThreshold: d("5000"),
		MinimumPayoutAmount: d("10000"),
	}
}

func newService(repo *fakeRepository) wallet.Service {
	return wallet.NewService(repo)
}

// --- NewService -------------------------------------------------------

func TestNewService_PanicsOnNilRepository(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	wallet.NewService(nil)
}

// --- Pure money math (wallet.go) -------------------------------------------

func TestCommissionFor(t *testing.T) {
	cases := []struct {
		name string
		fare string
		rate string
		want string
	}{
		{"simple 20 percent", "1000", "20", "200"},
		{"rounds to 3 decimals", "10.001", "33.333", "3.334"}, // 10.001*33.333/100 = 3.33363333.. -> 3.334
		{"zero fare", "0", "20", "0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wallet.CommissionFor(d(tc.fare), d(tc.rate))
			if !got.Equal(d(tc.want)) {
				t.Fatalf("CommissionFor(%s, %s) = %s, want %s", tc.fare, tc.rate, got, tc.want)
			}
		})
	}
}

func TestCommissionAndEarningAlwaysSumToFare(t *testing.T) {
	fares := []string{"1000", "999.999", "0.001", "12345.678", "1"}
	rate := d("17.5")

	for _, fareStr := range fares {
		fare := d(fareStr)
		commission := wallet.CommissionFor(fare, rate)
		earning := fare.Sub(commission)

		if !commission.Add(earning).Equal(fare) {
			t.Fatalf("fare=%s: commission(%s) + earning(%s) != fare", fareStr, commission, earning)
		}
	}
}

func TestConfig_SuspensionFloor(t *testing.T) {
	cfg := wallet.Config{SuspensionThreshold: d("5000")}
	if got := cfg.SuspensionFloor(); !got.Equal(d("-5000")) {
		t.Fatalf("got %s, want -5000", got)
	}
}

func TestConfig_IsSuspendedAt(t *testing.T) {
	cfg := wallet.Config{SuspensionThreshold: d("5000")} // floor = -5000

	cases := []struct {
		balance string
		want    bool
	}{
		{"-5000", true},  // exactly at floor
		{"-5000.001", true},
		{"-4999.999", false},
		{"0", false},
		{"1000", false},
	}

	for _, tc := range cases {
		t.Run(tc.balance, func(t *testing.T) {
			got := cfg.IsSuspendedAt(d(tc.balance))
			if got != tc.want {
				t.Fatalf("IsSuspendedAt(%s) = %v, want %v", tc.balance, got, tc.want)
			}
		})
	}
}

func TestConfig_AmountDueToReactivate(t *testing.T) {
	cfg := wallet.Config{SuspensionThreshold: d("5000")} // floor = -5000

	// AmountDueToReactivate always rounds UP to a whole currency unit
	// (Ceil, not a 3-decimal round) — see the method's doc comment for
	// why: a driver depositing exactly the shortfall would land right
	// back on the floor, still suspended.
	cases := []struct {
		name    string
		balance string
		want    string
	}{
		{"in good standing", "0", "0"},
		{"exactly at floor: shortfall is 0, still needs a whole unit", "-5000", "1"},
		{"shortfall just under a whole unit rounds down to it", "-5199.999", "200"},
		{"shortfall of exactly a whole unit still needs one more", "-5200", "201"},
		{"fractional shortfall rounds up to the next whole unit", "-5200.5", "201"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cfg.AmountDueToReactivate(d(tc.balance))
			if !got.Equal(d(tc.want)) {
				t.Fatalf("AmountDueToReactivate(%s) = %s, want %s", tc.balance, got, tc.want)
			}
		})
	}
}

func TestOwnerType_Valid(t *testing.T) {
	if !wallet.OwnerRider.Valid() || !wallet.OwnerDriver.Valid() {
		t.Fatal("expected OwnerRider and OwnerDriver to be valid")
	}
	if wallet.OwnerType("merchant").Valid() {
		t.Fatal("expected an unknown owner type to be invalid")
	}
}

func TestPaymentMethod_Valid(t *testing.T) {
	for _, m := range []wallet.PaymentMethod{wallet.PaymentCash, wallet.PaymentWallet, wallet.PaymentCard} {
		if !m.Valid() {
			t.Fatalf("expected %s to be valid", m)
		}
	}
	if wallet.PaymentMethod("crypto").Valid() {
		t.Fatal("expected an unknown payment method to be invalid")
	}
}

// --- GetWallet ----------------------------------------------------------

func TestService_GetWallet_ValidationErrors(t *testing.T) {
	svc := newService(newFakeRepository())

	if _, err := svc.GetWallet(context.Background(), "merchant", "id-1"); !errors.Is(err, wallet.ErrInvalidOwnerType) {
		t.Fatalf("got %v, want ErrInvalidOwnerType", err)
	}
	if _, err := svc.GetWallet(context.Background(), wallet.OwnerRider, "  "); !errors.Is(err, wallet.ErrOwnerIDRequired) {
		t.Fatalf("got %v, want ErrOwnerIDRequired", err)
	}
}

// --- TopUp ----------------------------------------------------------------

func TestService_TopUp_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   wallet.TopUpInput
		wantErr error
	}{
		{"invalid owner type", wallet.TopUpInput{OwnerType: "merchant", OwnerID: "id-1", Amount: d("100")}, wallet.ErrInvalidOwnerType},
		{"empty owner id", wallet.TopUpInput{OwnerType: wallet.OwnerRider, OwnerID: " ", Amount: d("100")}, wallet.ErrOwnerIDRequired},
		{"zero amount", wallet.TopUpInput{OwnerType: wallet.OwnerRider, OwnerID: "id-1", Amount: d("0")}, wallet.ErrInvalidAmount},
		{"negative amount", wallet.TopUpInput{OwnerType: wallet.OwnerRider, OwnerID: "id-1", Amount: d("-1")}, wallet.ErrInvalidAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			svc := newService(repo)

			_, _, err := svc.TopUp(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.applyMovementCalls) != 0 {
				t.Fatal("expected ApplyMovement not to be called on validation failure")
			}
		})
	}
}

func TestService_TopUp_DriverTopUpPassesSuspensionFloor(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig()
	svc := newService(repo)

	_, _, err := svc.TopUp(context.Background(), wallet.TopUpInput{
		OwnerType: wallet.OwnerDriver,
		OwnerID:   "driver-1",
		Amount:    d("6000"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.applyMovementCalls) != 1 {
		t.Fatalf("expected 1 ApplyMovement call, got %d", len(repo.applyMovementCalls))
	}

	call := repo.applyMovementCalls[0]
	if call.SuspensionFloor == nil {
		t.Fatal("expected SuspensionFloor to be set for a driver top-up")
	}
	if !call.SuspensionFloor.Equal(d("-5000")) {
		t.Fatalf("got floor %s, want -5000", call.SuspensionFloor)
	}
	if call.Description != "Driver commission deposit" {
		t.Fatalf("got description %q", call.Description)
	}
}

func TestService_TopUp_RiderTopUpDoesNotSetSuspensionFloor(t *testing.T) {
	repo := newFakeRepository()
	svc := newService(repo)

	_, _, err := svc.TopUp(context.Background(), wallet.TopUpInput{
		OwnerType: wallet.OwnerRider,
		OwnerID:   "rider-1",
		Amount:    d("1000"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.applyMovementCalls[0].SuspensionFloor != nil {
		t.Fatal("expected no SuspensionFloor for a rider top-up")
	}
	if repo.applyMovementCalls[0].Description != "Wallet top-up" {
		t.Fatalf("got description %q", repo.applyMovementCalls[0].Description)
	}
}

func TestService_TopUp_WrapsApplyMovementError(t *testing.T) {
	repo := newFakeRepository()
	repo.applyMovementErr = wallet.ErrDuplicateRequest
	svc := newService(repo)

	_, _, err := svc.TopUp(context.Background(), wallet.TopUpInput{
		OwnerType: wallet.OwnerRider,
		OwnerID:   "rider-1",
		Amount:    d("1000"),
	})
	if !errors.Is(err, wallet.ErrDuplicateRequest) {
		t.Fatalf("got %v, want wrapped ErrDuplicateRequest", err)
	}
}

// --- ListTransactions -----------------------------------------------------

func TestService_ListTransactions_ClampsLimit(t *testing.T) {
	cases := []struct {
		name  string
		given int
		want  int
	}{
		{"zero uses default", 0, 50},
		{"negative uses default", -5, 50},
		{"within range unchanged", 30, 30},
		{"above max is capped", 500, 200},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			svc := newService(repo)

			if _, err := svc.ListTransactions(context.Background(), wallet.OwnerRider, "rider-1", tc.given); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if repo.listTransactionsLimit != tc.want {
				t.Fatalf("got limit %d, want %d", repo.listTransactionsLimit, tc.want)
			}
		})
	}
}

// --- RequestPayout ----------------------------------------------------------

func TestService_RequestPayout_ValidationErrors(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig()
	svc := newService(repo)

	if _, _, err := svc.RequestPayout(context.Background(), wallet.PayoutInput{DriverID: " ", Amount: d("1000")}); !errors.Is(err, wallet.ErrDriverIDRequired) {
		t.Fatalf("got %v, want ErrDriverIDRequired", err)
	}
	if _, _, err := svc.RequestPayout(context.Background(), wallet.PayoutInput{DriverID: "driver-1", Amount: d("0")}); !errors.Is(err, wallet.ErrInvalidAmount) {
		t.Fatalf("got %v, want ErrInvalidAmount", err)
	}
}

func TestService_RequestPayout_RejectsBelowMinimum(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig() // minimum 10000
	svc := newService(repo)

	_, _, err := svc.RequestPayout(context.Background(), wallet.PayoutInput{
		DriverID: "driver-1",
		Amount:   d("9999.99"),
	})
	if !errors.Is(err, wallet.ErrBelowMinimumPayout) {
		t.Fatalf("got %v, want ErrBelowMinimumPayout", err)
	}
}

func TestService_RequestPayout_DebitsWithoutAllowNegative(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig()
	svc := newService(repo)

	_, _, err := svc.RequestPayout(context.Background(), wallet.PayoutInput{
		DriverID: "driver-1",
		Amount:   d("15000"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := repo.applyMovementCalls[0]
	if call.AllowNegative {
		t.Fatal("expected a payout to never allow a negative balance")
	}
	if !call.Amount.Equal(d("-15000")) {
		t.Fatalf("expected the movement amount to be negative, got %s", call.Amount)
	}
}

// --- SettleTrip -----------------------------------------------------------

func validSettleInput() wallet.SettleTripInput {
	return wallet.SettleTripInput{
		TripID:        "trip-1",
		RiderID:       "rider-1",
		DriverID:      "driver-1",
		FareAmount:    d("1000"),
		PaymentMethod: wallet.PaymentWallet,
	}
}

func TestService_SettleTrip_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(wallet.SettleTripInput) wallet.SettleTripInput
		wantErr error
	}{
		{"empty trip id", func(i wallet.SettleTripInput) wallet.SettleTripInput { i.TripID = ""; return i }, wallet.ErrTripIDRequired},
		{"empty rider id", func(i wallet.SettleTripInput) wallet.SettleTripInput { i.RiderID = ""; return i }, wallet.ErrRiderIDRequired},
		{"empty driver id", func(i wallet.SettleTripInput) wallet.SettleTripInput { i.DriverID = ""; return i }, wallet.ErrDriverIDRequired},
		{"invalid payment method", func(i wallet.SettleTripInput) wallet.SettleTripInput {
			i.PaymentMethod = "crypto"
			return i
		}, wallet.ErrInvalidPaymentMethod},
		{"negative fare", func(i wallet.SettleTripInput) wallet.SettleTripInput { i.FareAmount = d("-1"); return i }, wallet.ErrInvalidFareAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			svc := newService(repo)

			_, err := svc.SettleTrip(context.Background(), tc.mutate(validSettleInput()))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if len(repo.settleTripCalls) != 0 {
				t.Fatal("expected SettleTrip not to be called on validation failure")
			}
		})
	}
}

func TestService_SettleTrip_IsIdempotent(t *testing.T) {
	existing := wallet.Settlement{TripID: "trip-1", DriverEarning: d("800")}
	repo := newFakeRepository()
	repo.findSettlementResult = existing
	repo.findSettlementFound = true
	svc := newService(repo)

	got, err := svc.SettleTrip(context.Background(), validSettleInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != existing {
		t.Fatalf("got %+v, want the existing settlement unchanged", got)
	}

	if len(repo.settleTripCalls) != 0 {
		t.Fatal("expected a second SettleTrip call not to move any money")
	}
}

func TestService_SettleTrip_SplitsFareByConfiguredCommission(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig() // 20% commission, suspension threshold 5000

	svc := newService(repo)

	input := validSettleInput()
	input.FareAmount = d("1000")

	if _, err := svc.SettleTrip(context.Background(), input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.settleTripCalls) != 1 {
		t.Fatalf("expected 1 SettleTrip call, got %d", len(repo.settleTripCalls))
	}

	call := repo.settleTripCalls[0]
	if !call.CommissionAmount.Equal(d("200")) {
		t.Fatalf("got commission %s, want 200", call.CommissionAmount)
	}
	if !call.DriverEarning.Equal(d("800")) {
		t.Fatalf("got driver earning %s, want 800", call.DriverEarning)
	}
	if !call.SuspensionFloor.Equal(d("-5000")) {
		t.Fatalf("got suspension floor %s, want -5000", call.SuspensionFloor)
	}
}

// --- CheckDriverStanding ----------------------------------------------------

func TestService_CheckDriverStanding_RequiresDriverID(t *testing.T) {
	svc := newService(newFakeRepository())

	if _, err := svc.CheckDriverStanding(context.Background(), " "); !errors.Is(err, wallet.ErrDriverIDRequired) {
		t.Fatalf("got %v, want ErrDriverIDRequired", err)
	}
}

func TestService_CheckDriverStanding_HealthyBalance(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig()
	repo.wallets[walletKey(wallet.OwnerDriver, "driver-1")] = wallet.Wallet{
		OwnerType: wallet.OwnerDriver, OwnerID: "driver-1", Balance: d("1000"),
	}
	svc := newService(repo)

	got, err := svc.CheckDriverStanding(context.Background(), "driver-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.CanTakeTrips || got.Suspended {
		t.Fatalf("got %+v", got)
	}
	if !got.AmountDue.Equal(d("0")) {
		t.Fatalf("expected no amount due, got %s", got.AmountDue)
	}
}

func TestService_CheckDriverStanding_BlockedFlagWins(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig()
	repo.wallets[walletKey(wallet.OwnerDriver, "driver-1")] = wallet.Wallet{
		OwnerType: wallet.OwnerDriver, OwnerID: "driver-1", Balance: d("1000"), Blocked: true,
	}
	svc := newService(repo)

	got, err := svc.CheckDriverStanding(context.Background(), "driver-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.CanTakeTrips || !got.Suspended {
		t.Fatalf("got %+v", got)
	}
}

func TestService_CheckDriverStanding_SuspendedByBalanceEvenIfNotFlagged(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig() // floor -5000
	repo.wallets[walletKey(wallet.OwnerDriver, "driver-1")] = wallet.Wallet{
		OwnerType: wallet.OwnerDriver, OwnerID: "driver-1", Balance: d("-6000"), Blocked: false,
	}
	svc := newService(repo)

	got, err := svc.CheckDriverStanding(context.Background(), "driver-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.CanTakeTrips || !got.Suspended {
		t.Fatalf("got %+v", got)
	}
	if !got.AmountDue.Equal(d("1001")) {
		t.Fatalf("got amount due %s, want 1001", got.AmountDue)
	}
}
