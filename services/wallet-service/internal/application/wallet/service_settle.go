package wallet

import (
	"context"
	"fmt"
	"strings"
)

// SettleTrip splits one completed trip's fare between the driver and the
// platform. How the money actually moves depends on how the rider paid:
//
//	WALLET / CARD — the platform receives the fare, so it keeps its
//	commission and credits the driver the remainder. The driver's
//	balance goes UP by (fare - commission).
//
//	CASH — the driver has already taken the whole fare in hand, which
//	includes the platform's commission. The platform can't reach into
//	their pocket, so it debits the commission from their prepaid
//	balance instead. The driver's balance goes DOWN by the commission.
//
// Drivers work on a PREPAID commission balance: they deposit money up
// front, commission is drawn from it, and when the balance falls to the
// suspension threshold their account stops receiving trips until they
// top up again. In a cash-dominant market this is how the platform
// collects its cut at all — the driver holds the physical money, so the
// deposit is the platform's only real leverage.
//
// Settlement itself is never refused for a suspended driver: the trip
// already happened and the driver is owed their earning. Suspension
// stops FUTURE assignments, it doesn't retroactively void work done.
func (s *service) SettleTrip(
	ctx context.Context,
	input SettleTripInput,
) (Settlement, error) {
	tripID := strings.TrimSpace(input.TripID)
	if tripID == "" {
		return Settlement{}, ErrTripIDRequired
	}

	riderID := strings.TrimSpace(input.RiderID)
	if riderID == "" {
		return Settlement{}, ErrRiderIDRequired
	}

	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return Settlement{}, ErrDriverIDRequired
	}

	if !input.PaymentMethod.Valid() {
		return Settlement{}, ErrInvalidPaymentMethod
	}

	if input.FareAmount.IsNegative() {
		return Settlement{}, ErrInvalidFareAmount
	}

	// Idempotency: a redelivered trip.completed event must not pay the
	// driver twice.
	if existing, found, err := s.repository.FindSettlement(ctx, tripID); err != nil {
		return Settlement{}, fmt.Errorf("look up existing settlement: %w", err)
	} else if found {
		return existing, nil
	}

	config, err := s.repository.GetActiveConfig(ctx)
	if err != nil {
		return Settlement{}, fmt.Errorf("get active wallet config: %w", err)
	}

	commission := CommissionFor(input.FareAmount, config.CommissionRate)

	// Derived by subtraction rather than a second multiplication, so
	// commission + earning always equals the fare exactly.
	driverEarning := input.FareAmount.Sub(commission)

	settlement, err := s.repository.SettleTrip(ctx, SettleInput{
		TripID:           tripID,
		RiderID:          riderID,
		DriverID:         driverID,
		CurrencyCode:     config.CurrencyCode,
		PaymentMethod:    input.PaymentMethod,
		FareAmount:       input.FareAmount,
		CommissionRate:   config.CommissionRate,
		CommissionAmount: commission,
		DriverEarning:    driverEarning,
		SuspensionFloor:  config.SuspensionFloor(),
	})
	if err != nil {
		return Settlement{}, fmt.Errorf("settle trip: %w", err)
	}

	return settlement, nil
}

// CheckDriverStanding is what Dispatch must call before assigning ANY
// trip — not just cash ones. A driver whose prepaid balance has fallen
// to the suspension threshold receives no work until they deposit more.
func (s *service) CheckDriverStanding(
	ctx context.Context,
	driverID string,
) (DriverStanding, error) {
	trimmedID := strings.TrimSpace(driverID)
	if trimmedID == "" {
		return DriverStanding{}, ErrDriverIDRequired
	}

	config, err := s.repository.GetActiveConfig(ctx)
	if err != nil {
		return DriverStanding{}, fmt.Errorf("get active wallet config: %w", err)
	}

	driverWallet, err := s.repository.FindOrCreateWallet(
		ctx,
		OwnerDriver,
		trimmedID,
		config.CurrencyCode,
	)
	if err != nil {
		return DriverStanding{}, fmt.Errorf("get driver wallet: %w", err)
	}

	standing := DriverStanding{
		CurrentBalance:      driverWallet.Balance,
		SuspensionThreshold: config.SuspensionFloor(),
		AmountDue:           config.AmountDueToReactivate(driverWallet.Balance),
	}

	switch {
	case driverWallet.Blocked:
		standing.Suspended = true
		standing.Reason = "account suspended: deposit required to resume receiving trips"
	case config.IsSuspendedAt(driverWallet.Balance):
		// Defensive: the balance says suspended even though the flag
		// wasn't set. Report the truth rather than the stale flag.
		standing.Suspended = true
		standing.Reason = "balance is at or below the suspension threshold"
	default:
		standing.CanTakeTrips = true
	}

	return standing, nil
}
