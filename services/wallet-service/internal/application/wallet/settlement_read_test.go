package wallet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// settlementRepoFake implements the whole Repository, but only FindSettlement
// does anything: it is a self-contained double for the settlement read.
type settlementRepoFake struct {
	settlement wallet.Settlement
	found      bool
	err        error

	askedFor []string
}

func (r *settlementRepoFake) GetActiveConfig(context.Context) (wallet.Config, error) {
	return wallet.Config{}, nil
}

func (r *settlementRepoFake) FindOrCreateWallet(
	context.Context, wallet.OwnerType, string, string,
) (wallet.Wallet, error) {
	return wallet.Wallet{}, nil
}

func (r *settlementRepoFake) FindWallet(context.Context, wallet.OwnerType, string) (wallet.Wallet, error) {
	return wallet.Wallet{}, nil
}

func (r *settlementRepoFake) ApplyMovement(
	context.Context, wallet.MovementInput,
) (wallet.Wallet, wallet.Transaction, error) {
	return wallet.Wallet{}, wallet.Transaction{}, nil
}

func (r *settlementRepoFake) FindSettlement(_ context.Context, tripID string) (wallet.Settlement, bool, error) {
	r.askedFor = append(r.askedFor, tripID)

	return r.settlement, r.found, r.err
}

func (r *settlementRepoFake) SettleTrip(context.Context, wallet.SettleInput) (wallet.Settlement, error) {
	return wallet.Settlement{}, nil
}

func (r *settlementRepoFake) ListTransactions(
	context.Context, wallet.OwnerType, string, int,
) ([]wallet.Transaction, error) {
	return nil, nil
}

const (
	settlementTripID   = "3f2b6a7e-1c1d-4b5e-9a3f-0d8c6b1a2e4f"
	settlementRiderID  = "d586ce00-5c1c-46f1-81b5-ed7e0977d075"
	settlementDriverID = "fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
	otherPersonID      = "4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
)

func settledTripRepo() *settlementRepoFake {
	return &settlementRepoFake{
		found: true,
		settlement: wallet.Settlement{
			TripID:           settlementTripID,
			RiderID:          settlementRiderID,
			DriverID:         settlementDriverID,
			PaymentMethod:    wallet.PaymentWallet,
			FareAmount:       decimal.RequireFromString("5750"),
			CommissionAmount: decimal.RequireFromString("1150"),
			DriverEarning:    decimal.RequireFromString("4600"),
			WalletAmount:     decimal.RequireFromString("2000"),
			CashAmount:       decimal.RequireFromString("3750"),
		},
	}
}

func TestGetTripSettlement_TheTwoPeopleOnTheTripSeeIt(t *testing.T) {
	cases := []struct {
		name      string
		ownerType wallet.OwnerType
		ownerID   string
	}{
		{"the rider", wallet.OwnerRider, settlementRiderID},
		{"the driver", wallet.OwnerDriver, settlementDriverID},
		{"the rider, with an upper-case id", wallet.OwnerRider, "D586CE00-5C1C-46F1-81B5-ED7E0977D075"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := wallet.NewService(settledTripRepo())

			got, err := svc.GetTripSettlement(context.Background(), tc.ownerType, tc.ownerID, settlementTripID)
			if err != nil {
				t.Fatalf("GetTripSettlement: %v", err)
			}

			if !got.WalletAmount.Equal(decimal.RequireFromString("2000")) ||
				!got.CashAmount.Equal(decimal.RequireFromString("3750")) {
				t.Errorf("split = %s wallet / %s cash, want 2000 / 3750", got.WalletAmount, got.CashAmount)
			}
		})
	}
}

func TestGetTripSettlement_EveryoneElseGetsNotFound(t *testing.T) {
	cases := []struct {
		name      string
		ownerType wallet.OwnerType
		ownerID   string
	}{
		{"a different rider", wallet.OwnerRider, otherPersonID},
		{"a different driver", wallet.OwnerDriver, otherPersonID},
		{"the rider's id presented as a driver", wallet.OwnerDriver, settlementRiderID},
		{"the driver's id presented as a rider", wallet.OwnerRider, settlementDriverID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := wallet.NewService(settledTripRepo())

			_, err := svc.GetTripSettlement(context.Background(), tc.ownerType, tc.ownerID, settlementTripID)
			if !errors.Is(err, wallet.ErrSettlementNotFound) {
				t.Fatalf("got %v, want ErrSettlementNotFound", err)
			}
		})
	}
}

func TestGetTripSettlement_NoSettlementYetIsNotFound(t *testing.T) {
	svc := wallet.NewService(&settlementRepoFake{found: false})

	_, err := svc.GetTripSettlement(context.Background(), wallet.OwnerRider, settlementRiderID, settlementTripID)
	if !errors.Is(err, wallet.ErrSettlementNotFound) {
		t.Fatalf("got %v, want ErrSettlementNotFound", err)
	}
}

func TestGetTripSettlement_ATripIdThatCannotExistNeverReachesTheDatabase(t *testing.T) {
	repo := settledTripRepo()
	svc := wallet.NewService(repo)

	_, err := svc.GetTripSettlement(context.Background(), wallet.OwnerRider, settlementRiderID, "not-a-uuid")
	if !errors.Is(err, wallet.ErrSettlementNotFound) {
		t.Fatalf("got %v, want ErrSettlementNotFound", err)
	}

	if len(repo.askedFor) != 0 {
		t.Errorf("the repository was asked for %v", repo.askedFor)
	}
}

func TestGetTripSettlement_ValidationErrors(t *testing.T) {
	svc := wallet.NewService(settledTripRepo())

	cases := []struct {
		name      string
		ownerType wallet.OwnerType
		ownerID   string
		tripID    string
		want      error
	}{
		{"unknown owner type", wallet.OwnerType("admin"), settlementRiderID, settlementTripID, wallet.ErrInvalidOwnerType},
		{"blank owner id", wallet.OwnerRider, "  ", settlementTripID, wallet.ErrOwnerIDRequired},
		{"blank trip id", wallet.OwnerRider, settlementRiderID, "", wallet.ErrTripIDRequired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.GetTripSettlement(context.Background(), tc.ownerType, tc.ownerID, tc.tripID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGetTripSettlement_RepositoryFailureIsNotHiddenAsNotFound(t *testing.T) {
	boom := errors.New("database is down")
	svc := wallet.NewService(&settlementRepoFake{err: boom})

	_, err := svc.GetTripSettlement(context.Background(), wallet.OwnerRider, settlementRiderID, settlementTripID)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the repository error", err)
	}

	if errors.Is(err, wallet.ErrSettlementNotFound) {
		t.Fatal("a database failure must not look like a missing settlement")
	}
}
