package wallet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TxChangeCredit is the ledger type of the change a driver could not return in cash.
const TxChangeCredit TransactionType = "change_credit"

var (
	// ErrNoCashDue: the trip had no cash part (a full wallet or card trip), so there is
	// no cash a rider could have overpaid.
	ErrNoCashDue = errors.New("this trip has no cash part to give change on")

	// ErrNoChangeOwed: the cash received is not more than the cash due.
	ErrNoChangeOwed = errors.New("the cash received must be more than the cash due")

	// ErrChangeAboveLimit: the change is more than the deployment allows per trip.
	ErrChangeAboveLimit = errors.New("the change is above the limit")

	// ErrChangeAlreadyRecorded: a different amount was already recorded for this trip.
	ErrChangeAlreadyRecorded = errors.New("a different change amount was already recorded for this trip")
)

// RecordTripChangeInput is what the driver reports: all the cash the rider handed over.
// The change itself is never sent by the client: it is worked out here from the settlement.
type RecordTripChangeInput struct {
	DriverID     string
	TripID       string
	CashReceived Money
}

// ChangeCreditInput is a validated credit, ready to be written.
type ChangeCreditInput struct {
	TripID       string
	RiderID      string
	DriverID     string
	CurrencyCode string
	CashDue      Money
	CashReceived Money
	ChangeAmount Money
}

// ChangeCredit is a recorded credit. Repeated is true when the same amount had already
// been recorded for the trip, so nothing was credited again.
type ChangeCredit struct {
	TripID       string
	RiderID      string
	DriverID     string
	CurrencyCode string
	CashDue      Money
	CashReceived Money
	ChangeAmount Money
	RiderBalance Money
	CreatedAt    time.Time
	Repeated     bool
}

// RecordTripChange credits the rider's wallet with the change the driver could not return.
// Only the trip's driver may record it, only after the trip is settled, once per trip
// (recording the same amount again changes nothing), and never above the configured limit.
// The platform pays: nothing is taken from the driver.
func (s *service) RecordTripChange(
	ctx context.Context,
	input RecordTripChangeInput,
) (ChangeCredit, error) {
	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return ChangeCredit{}, ErrDriverIDRequired
	}

	tripID := strings.TrimSpace(input.TripID)
	if tripID == "" {
		return ChangeCredit{}, ErrTripIDRequired
	}

	if !uuidShape.MatchString(tripID) {
		return ChangeCredit{}, ErrSettlementNotFound
	}

	if !input.CashReceived.IsPositive() {
		return ChangeCredit{}, ErrInvalidAmount
	}

	settlement, found, err := s.repository.FindSettlement(ctx, tripID)
	if err != nil {
		return ChangeCredit{}, fmt.Errorf("find trip settlement: %w", err)
	}

	// Anyone but the trip's own driver sees exactly what they would for a trip that
	// does not exist.
	if !found || !strings.EqualFold(settlement.DriverID, driverID) {
		return ChangeCredit{}, ErrSettlementNotFound
	}

	if !settlement.CashAmount.IsPositive() {
		return ChangeCredit{}, ErrNoCashDue
	}

	change := input.CashReceived.Sub(settlement.CashAmount)
	if !change.IsPositive() {
		return ChangeCredit{}, ErrNoChangeOwed
	}

	config, err := s.repository.GetActiveConfig(ctx)
	if err != nil {
		return ChangeCredit{}, fmt.Errorf("get active wallet config: %w", err)
	}

	if change.GreaterThan(config.MaxChangeCredit) {
		return ChangeCredit{}, fmt.Errorf("%w: at most %s", ErrChangeAboveLimit, config.MaxChangeCredit.String())
	}

	credit, err := s.repository.CreditTripChange(ctx, ChangeCreditInput{
		TripID:       settlement.TripID,
		RiderID:      settlement.RiderID,
		DriverID:     settlement.DriverID,
		CurrencyCode: settlement.CurrencyCode,
		CashDue:      settlement.CashAmount,
		CashReceived: input.CashReceived,
		ChangeAmount: change,
	})
	if err != nil {
		return ChangeCredit{}, fmt.Errorf("credit trip change: %w", err)
	}

	return credit, nil
}
