package wallet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// changeRepoFake reuses the settlement double and adds what the change credit needs: the
// configured limit, and a record of what was credited.
type changeRepoFake struct {
	*settlementRepoFake

	config    wallet.Config
	credited  []wallet.ChangeCreditInput
	creditErr error
}

func (r *changeRepoFake) GetActiveConfig(context.Context) (wallet.Config, error) {
	return r.config, nil
}

func (r *changeRepoFake) CreditTripChange(
	_ context.Context,
	in wallet.ChangeCreditInput,
) (wallet.ChangeCredit, error) {
	r.credited = append(r.credited, in)

	if r.creditErr != nil {
		return wallet.ChangeCredit{}, r.creditErr
	}

	return wallet.ChangeCredit{
		TripID:       in.TripID,
		RiderID:      in.RiderID,
		DriverID:     in.DriverID,
		CurrencyCode: in.CurrencyCode,
		CashDue:      in.CashDue,
		CashReceived: in.CashReceived,
		ChangeAmount: in.ChangeAmount,
	}, nil
}

// cashTripRepo is a settled trip whose cash part is cashDue, with a change limit of 2000.
func cashTripRepo(cashDue string) *changeRepoFake {
	return &changeRepoFake{
		settlementRepoFake: &settlementRepoFake{
			found: true,
			settlement: wallet.Settlement{
				TripID:       settlementTripID,
				RiderID:      settlementRiderID,
				DriverID:     settlementDriverID,
				CurrencyCode: "IQD",
				FareAmount:   decimal.RequireFromString("17250"),
				CashAmount:   decimal.RequireFromString(cashDue),
			},
		},
		config: wallet.Config{MaxChangeCredit: decimal.RequireFromString("2000")},
	}
}

func recordChange(svc wallet.Service, driverID, tripID, received string) (wallet.ChangeCredit, error) {
	value, err := decimal.NewFromString(received)
	if err != nil {
		panic(err)
	}

	return svc.RecordTripChange(context.Background(), wallet.RecordTripChangeInput{
		DriverID:     driverID,
		TripID:       tripID,
		CashReceived: value,
	})
}

func TestRecordTripChange_CreditsTheDifference(t *testing.T) {
	repo := cashTripRepo("16750")
	svc := wallet.NewService(repo)

	got, err := recordChange(svc, settlementDriverID, settlementTripID, "17000")
	if err != nil {
		t.Fatalf("RecordTripChange: %v", err)
	}

	if !got.ChangeAmount.Equal(decimal.RequireFromString("250")) {
		t.Errorf("change = %s, want 250", got.ChangeAmount)
	}

	if len(repo.credited) != 1 {
		t.Fatalf("credits written = %d, want 1", len(repo.credited))
	}

	in := repo.credited[0]

	if in.RiderID != settlementRiderID || in.DriverID != settlementDriverID || in.TripID != settlementTripID {
		t.Errorf("credit for the wrong people or trip: %+v", in)
	}

	if in.CurrencyCode != "IQD" {
		t.Errorf("currency = %q, want IQD", in.CurrencyCode)
	}

	if !in.CashDue.Equal(decimal.RequireFromString("16750")) || !in.CashReceived.Equal(decimal.RequireFromString("17000")) {
		t.Errorf("cash due/received = %s/%s, want 16750/17000", in.CashDue, in.CashReceived)
	}
}

func TestRecordTripChange_TheDriverIdIsMatchedWithoutCase(t *testing.T) {
	svc := wallet.NewService(cashTripRepo("16750"))

	if _, err := recordChange(svc, "FE94A3D3-F10D-4C3E-853D-3D1302A5FEB5", settlementTripID, "17000"); err != nil {
		t.Fatalf("RecordTripChange: %v", err)
	}
}

func TestRecordTripChange_TheLimitIsInclusive(t *testing.T) {
	cases := []struct {
		name     string
		received string
		wantErr  error
	}{
		{"exactly the limit", "18750", nil},
		{"one dinar over the limit", "18751", wallet.ErrChangeAboveLimit},
		{"far over the limit", "30000", wallet.ErrChangeAboveLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cashTripRepo("16750")
			svc := wallet.NewService(repo)

			_, err := recordChange(svc, settlementDriverID, settlementTripID, tc.received)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}

			if tc.wantErr != nil && len(repo.credited) != 0 {
				t.Error("a credit was written above the limit")
			}
		})
	}
}

func TestRecordTripChange_NoChangeIsOwedWhenTheCashIsNotMore(t *testing.T) {
	for _, received := range []string{"16750", "16000"} {
		repo := cashTripRepo("16750")
		svc := wallet.NewService(repo)

		_, err := recordChange(svc, settlementDriverID, settlementTripID, received)
		if !errors.Is(err, wallet.ErrNoChangeOwed) {
			t.Errorf("received %s: got %v, want ErrNoChangeOwed", received, err)
		}

		if len(repo.credited) != 0 {
			t.Errorf("received %s: a credit was written", received)
		}
	}
}

func TestRecordTripChange_ATripWithNoCashPartHasNoChange(t *testing.T) {
	repo := cashTripRepo("0")
	svc := wallet.NewService(repo)

	_, err := recordChange(svc, settlementDriverID, settlementTripID, "1000")
	if !errors.Is(err, wallet.ErrNoCashDue) {
		t.Fatalf("got %v, want ErrNoCashDue", err)
	}
}

func TestRecordTripChange_OnlyTheTripsOwnDriverCanRecordIt(t *testing.T) {
	cases := []struct {
		name     string
		driverID string
		tripID   string
		found    bool
	}{
		{"another driver", otherPersonID, settlementTripID, true},
		{"the rider's id as the driver", settlementRiderID, settlementTripID, true},
		{"a trip with no settlement", settlementDriverID, settlementTripID, false},
		{"an id that is not a UUID", settlementDriverID, "not-a-uuid", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := cashTripRepo("16750")
			repo.found = tc.found
			svc := wallet.NewService(repo)

			_, err := recordChange(svc, tc.driverID, tc.tripID, "17000")
			if !errors.Is(err, wallet.ErrSettlementNotFound) {
				t.Fatalf("got %v, want ErrSettlementNotFound", err)
			}

			if len(repo.credited) != 0 {
				t.Error("a credit was written")
			}
		})
	}
}

func TestRecordTripChange_ValidationErrors(t *testing.T) {
	svc := wallet.NewService(cashTripRepo("16750"))

	cases := []struct {
		name     string
		driverID string
		tripID   string
		received string
		want     error
	}{
		{"blank driver id", "  ", settlementTripID, "17000", wallet.ErrDriverIDRequired},
		{"blank trip id", settlementDriverID, "", "17000", wallet.ErrTripIDRequired},
		{"zero cash", settlementDriverID, settlementTripID, "0", wallet.ErrInvalidAmount},
		{"negative cash", settlementDriverID, settlementTripID, "-5", wallet.ErrInvalidAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := recordChange(svc, tc.driverID, tc.tripID, tc.received)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRecordTripChange_RepositoryOutcomesReachTheCaller(t *testing.T) {
	boom := errors.New("database is down")

	for _, repoErr := range []error{wallet.ErrChangeAlreadyRecorded, boom} {
		repo := cashTripRepo("16750")
		repo.creditErr = repoErr
		svc := wallet.NewService(repo)

		_, err := recordChange(svc, settlementDriverID, settlementTripID, "17000")
		if !errors.Is(err, repoErr) {
			t.Errorf("got %v, want %v", err, repoErr)
		}
	}
}
