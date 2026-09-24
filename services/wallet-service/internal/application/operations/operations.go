// Package operations is money staff move by hand: corrections of a balance,
// refunds of a trip, and the payouts drivers ask for.
package operations

import (
	"context"
	"errors"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var (
	ErrStaffRequired      = errors.New("this is done by a staff member")
	ErrOwnerRequired      = errors.New("owner_type and owner_id are required")
	ErrTripRequired       = errors.New("trip_id is required")
	ErrDriverRequired     = errors.New("driver_id is required")
	ErrInvalidAmount      = errors.New("amount must be a decimal with at most 3 decimal places, and not zero")
	ErrInvalidRefund      = errors.New("amount must be positive, and driver_amount between 0 and amount (at most 3 decimal places)")
	ErrReasonRequired     = errors.New("reason is required (3-300 characters)")
	ErrIdempotencyKey     = errors.New("idempotency_key is required (1-120 characters)")
	ErrKeyReused          = errors.New("this idempotency_key was already used for another operation")
	ErrDestinationTooLong = errors.New("destination must be at most 120 characters")
	ErrReferenceRequired  = errors.New("reference is required (1-120 characters)")
	ErrInvalidStatus      = errors.New("status must be pending, approved, paid or rejected")
	ErrInvalidPageToken   = errors.New("page_token is not a token from a previous page")

	ErrTripNotSettled = errors.New("the trip is not settled: there is nothing to refund yet")
	// ErrRefundTooLarge: the trip's refunds would pass its fare (or fee).
	ErrRefundTooLarge = errors.New("the refunds of this trip would be more than its fare (or fee)")

	ErrPayoutNotFound = errors.New("payout request not found")
	ErrPayoutOpen     = errors.New("the driver has a payout request still open")
	// ErrPayoutState: approved only when pending; paid only when pending or
	// approved; rejected only before it is paid.
	ErrPayoutState = errors.New("the payout request is not in a state that allows this")
)

// Kind is what an adjustment row is.
type Kind string

const (
	KindAdjustment Kind = "adjustment"
	KindRefund     Kind = "refund"
)

// Adjustment is a staff member's correction of a balance, or a refund.
type Adjustment struct {
	ID            string
	Kind          Kind
	OwnerType     wallet.OwnerType
	OwnerID       string
	CurrencyCode  string
	Amount        wallet.Money
	TripID        string
	DriverID      string
	DriverAmount  wallet.Money
	Reason        string
	TransactionID string
	CreatedBy     string
	CreatedAt     time.Time
}

// PayoutStatus is where a payout request stands.
type PayoutStatus string

const (
	PayoutPending  PayoutStatus = "pending"
	PayoutApproved PayoutStatus = "approved"
	PayoutPaid     PayoutStatus = "paid"
	PayoutRejected PayoutStatus = "rejected"
)

// Payout is a driver asking for their money.
type Payout struct {
	ID             string
	DriverID       string
	CurrencyCode   string
	Amount         wallet.Money
	Destination    string
	Status         PayoutStatus
	IdempotencyKey string
	CreatedAt      time.Time
	ApprovedAt     *time.Time
	PaidAt         *time.Time
	PaidReference  string
	RejectedAt     *time.Time
	RejectReason   string
}

// AdjustRecord is what the store writes for an adjustment.
type AdjustRecord struct {
	OwnerType      wallet.OwnerType
	OwnerID        string
	Amount         wallet.Money
	Reason         string
	CreatedBy      string
	IdempotencyKey string
}

// RefundRecord is what the store writes for a refund.
type RefundRecord struct {
	TripID         string
	Amount         wallet.Money
	DriverAmount   wallet.Money
	Reason         string
	CreatedBy      string
	IdempotencyKey string
}

// PayoutRecord is a driver's new payout request.
type PayoutRecord struct {
	DriverID       string
	Amount         wallet.Money
	Destination    string
	IdempotencyKey string
}

// TripRefunds is a trip's refunds and its fare (or fee): the most they may add up to.
type TripRefunds struct {
	Refunds  []Adjustment
	Charged  wallet.Money
	Refunded wallet.Money
}

// Store writes the operations with their ledger rows, each in one
// transaction with the wallets locked.
type Store interface {
	Config(ctx context.Context) (wallet.Config, error)

	FindAdjustmentByKey(ctx context.Context, createdBy, key string) (Adjustment, bool, error)
	// Adjust moves the amount with an adjustment row. A rider's debit past
	// the balance is wallet.ErrInsufficientFunds; a driver's may go below
	// zero (their suspension follows). wallet.ErrDuplicateRequest when the
	// key was taken meanwhile.
	Adjust(ctx context.Context, record AdjustRecord) (Adjustment, wallet.Wallet, error)
	// Refund credits the trip's rider (and debits its driver the
	// driver_amount) with the trip's settlement locked, so the refunds never
	// add up past the trip's fare or fee (ErrRefundTooLarge).
	// ErrTripNotSettled when there is no settlement.
	Refund(ctx context.Context, record RefundRecord) (Adjustment, wallet.Wallet, error)
	TripRefunds(ctx context.Context, tripID string) (TripRefunds, error)

	FindPayoutByKey(ctx context.Context, driverID, key string) (Payout, bool, error)
	// RequestPayout holds the amount (a payout row) and records the request.
	// ErrPayoutOpen when one is open; wallet.ErrInsufficientFunds,
	// wallet.ErrWalletBlocked.
	RequestPayout(ctx context.Context, record PayoutRecord) (Payout, wallet.Wallet, wallet.Transaction, error)
	GetPayout(ctx context.Context, id string) (Payout, error)
	// ListPayouts: driverID "" for every driver; status "" for all. A
	// driver's newest first; the queue oldest first.
	ListPayouts(ctx context.Context, driverID string, status PayoutStatus, offset, limit int) ([]Payout, error)
	// SetPayoutStatus moves a request (locked) to approved or paid, from the
	// states that allow it; ErrPayoutState otherwise.
	SetPayoutStatus(ctx context.Context, id string, to PayoutStatus, by, reference string, now time.Time) (Payout, error)
	// RejectPayout returns the held amount to the driver's wallet and closes
	// the request; ErrPayoutState once it is paid or rejected.
	RejectPayout(ctx context.Context, id, by, reason string, now time.Time) (Payout, error)
}

// Inspection is a wallet as staff see it.
type Inspection struct {
	Wallet          wallet.Wallet
	Recent          []wallet.Transaction
	OutstandingDues wallet.Money
	OpenPayouts     []Payout
}
