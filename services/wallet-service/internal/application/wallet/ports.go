package wallet

import "context"

// CreditInput and DebitInput describe a single balance movement plus the
// ledger row that records it. The repository applies both atomically.
type MovementInput struct {
	OwnerType      OwnerType
	OwnerID        string
	Type           TransactionType
	Amount         Money // signed
	TripID         string
	TransferID     string
	IdempotencyKey string
	Description    string

	// AllowNegative permits the resulting balance to go below zero. True
	// for driver commission deductions, since a driver's prepaid balance
	// is allowed a grace window past zero before suspension.
	AllowNegative bool

	// SuspensionFloor, when set on a driver movement, makes the
	// repository re-evaluate the account's blocked flag after the
	// balance changes: suspend at or below the floor, reinstate above
	// it. This keeps suspension state consistent with the balance
	// automatically, rather than relying on a separate job or on
	// callers remembering to check.
	SuspensionFloor *Money
}

type SettleInput struct {
	TripID           string
	RiderID          string
	DriverID         string
	CurrencyCode     string
	PaymentMethod    PaymentMethod
	FareAmount       Money
	CommissionRate   Money
	CommissionAmount Money
	DriverEarning    Money
	SuspensionFloor  Money
	Kind             SettlementKind
}

type Repository interface {
	GetActiveConfig(ctx context.Context) (Config, error)

	// FindOrCreateWallet returns the owner's wallet, creating an empty
	// one on first use. Wallets are created lazily rather than requiring
	// rider-service/driver-service to call us at signup — one less
	// cross-service coupling to keep in sync.
	FindOrCreateWallet(
		ctx context.Context,
		ownerType OwnerType,
		ownerID string,
		currencyCode string,
	) (Wallet, error)

	FindWallet(
		ctx context.Context,
		ownerType OwnerType,
		ownerID string,
	) (Wallet, error)

	// ApplyMovement adjusts a balance and appends the matching ledger
	// row in one transaction. Returns ErrDuplicateRequest if the
	// idempotency key was already used, and ErrInsufficientFunds if the
	// movement would make the balance negative without AllowNegative.
	ApplyMovement(
		ctx context.Context,
		input MovementInput,
	) (Wallet, Transaction, error)

	// FindSettlement supports SettleTrip's idempotency.
	FindSettlement(ctx context.Context, tripID string) (Settlement, bool, error)

	// SettleTrip moves the rider's payment, the driver's earning, and
	// the platform's commission in ONE transaction, along with the
	// settlement record and outbox event. A trip's money must never be
	// half-moved.
	SettleTrip(ctx context.Context, input SettleInput) (Settlement, error)

	ListTransactions(
		ctx context.Context,
		ownerType OwnerType,
		ownerID string,
		limit int,
	) ([]Transaction, error)

	// CreditTripChange records the change and credits the rider's wallet in one
	// transaction, at most once per trip: the same amount again returns the first record
	// (Repeated) and a different amount returns ErrChangeAlreadyRecorded.
	CreditTripChange(ctx context.Context, input ChangeCreditInput) (ChangeCredit, error)
}
