package wallet

import (
	"context"
	"fmt"
	"strings"
)

const defaultTransactionLimit = 50
const maxTransactionLimit = 200

func (s *service) GetWallet(
	ctx context.Context,
	ownerType OwnerType,
	ownerID string,
) (Wallet, error) {
	if !ownerType.Valid() {
		return Wallet{}, ErrInvalidOwnerType
	}

	trimmedID := strings.TrimSpace(ownerID)
	if trimmedID == "" {
		return Wallet{}, ErrOwnerIDRequired
	}

	config, err := s.repository.GetActiveConfig(ctx)
	if err != nil {
		return Wallet{}, fmt.Errorf("get active wallet config: %w", err)
	}

	found, err := s.repository.FindOrCreateWallet(ctx, ownerType, trimmedID, config.CurrencyCode)
	if err != nil {
		return Wallet{}, fmt.Errorf("get wallet: %w", err)
	}

	return found, nil
}

func (s *service) TopUp(
	ctx context.Context,
	input TopUpInput,
) (Wallet, Transaction, error) {
	if !input.OwnerType.Valid() {
		return Wallet{}, Transaction{}, ErrInvalidOwnerType
	}

	ownerID := strings.TrimSpace(input.OwnerID)
	if ownerID == "" {
		return Wallet{}, Transaction{}, ErrOwnerIDRequired
	}

	if !input.Amount.IsPositive() {
		return Wallet{}, Transaction{}, ErrInvalidAmount
	}

	description := input.Description
	if strings.TrimSpace(description) == "" {
		if input.OwnerType == OwnerDriver {
			description = "Driver commission deposit"
		} else {
			description = "Wallet top-up"
		}
	}

	movement := MovementInput{
		OwnerType:      input.OwnerType,
		OwnerID:        ownerID,
		Type:           TxTopUp,
		Amount:         input.Amount,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		Description:    description,
	}

	// A driver topping up is depositing against their prepaid commission
	// balance — the whole point is usually to get back above the
	// suspension threshold and start receiving trips again. Passing the
	// floor lets the repository lift the suspension in the same
	// transaction as the deposit, so there's no window where the money
	// is in but the account is still blocked.
	if input.OwnerType == OwnerDriver {
		config, err := s.repository.GetActiveConfig(ctx)
		if err != nil {
			return Wallet{}, Transaction{}, fmt.Errorf("get active wallet config: %w", err)
		}

		floor := config.SuspensionFloor()
		movement.SuspensionFloor = &floor
	}

	updated, transaction, err := s.repository.ApplyMovement(ctx, movement)
	if err != nil {
		return Wallet{}, Transaction{}, fmt.Errorf("apply top-up: %w", err)
	}

	return updated, transaction, nil
}

func (s *service) ListTransactions(
	ctx context.Context,
	ownerType OwnerType,
	ownerID string,
	limit int,
) ([]Transaction, error) {
	if !ownerType.Valid() {
		return nil, ErrInvalidOwnerType
	}

	trimmedID := strings.TrimSpace(ownerID)
	if trimmedID == "" {
		return nil, ErrOwnerIDRequired
	}

	if limit <= 0 {
		limit = defaultTransactionLimit
	}

	if limit > maxTransactionLimit {
		limit = maxTransactionLimit
	}

	transactions, err := s.repository.ListTransactions(ctx, ownerType, trimmedID, limit)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}

	return transactions, nil
}

// RequestPayout moves money out of a driver's wallet toward their bank
// account. This service only records the intent and debits the balance —
// actually moving money to a bank is a separate concern handled outside
// this platform (a payment provider, or a manual bank transfer by the
// operator). The ledger row is the instruction and the audit trail.
func (s *service) RequestPayout(
	ctx context.Context,
	input PayoutInput,
) (Wallet, Transaction, error) {
	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return Wallet{}, Transaction{}, ErrDriverIDRequired
	}

	if !input.Amount.IsPositive() {
		return Wallet{}, Transaction{}, ErrInvalidAmount
	}

	config, err := s.repository.GetActiveConfig(ctx)
	if err != nil {
		return Wallet{}, Transaction{}, fmt.Errorf("get active wallet config: %w", err)
	}

	if input.Amount.LessThan(config.MinimumPayoutAmount) {
		return Wallet{}, Transaction{}, ErrBelowMinimumPayout
	}

	// Payouts never use AllowNegative — a driver can only withdraw money
	// they actually have. The repository rejects the movement if the
	// balance would go below zero.
	updated, transaction, err := s.repository.ApplyMovement(ctx, MovementInput{
		OwnerType:      OwnerDriver,
		OwnerID:        driverID,
		Type:           TxPayout,
		Amount:         input.Amount.Neg(),
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		Description:    "Payout to driver",
	})
	if err != nil {
		return Wallet{}, Transaction{}, fmt.Errorf("apply payout: %w", err)
	}

	return updated, transaction, nil
}
