package wallet

import "context"

type TopUpInput struct {
	OwnerType      OwnerType
	OwnerID        string
	Amount         Money
	IdempotencyKey string
	Description    string
}

type SettleTripInput struct {
	TripID        string
	RiderID       string
	DriverID      string
	FareAmount    Money
	PaymentMethod PaymentMethod
}

type PayoutInput struct {
	DriverID       string
	Amount         Money
	IdempotencyKey string
}

// DriverStanding answers "should this driver be given work right now".
type DriverStanding struct {
	CanTakeTrips        bool
	Suspended           bool
	CurrentBalance      Money
	SuspensionThreshold Money
	AmountDue           Money
	Reason              string
}

type Service interface {
	GetWallet(ctx context.Context, ownerType OwnerType, ownerID string) (Wallet, error)
	TopUp(ctx context.Context, input TopUpInput) (Wallet, Transaction, error)
	SettleTrip(ctx context.Context, input SettleTripInput) (Settlement, error)
	ListTransactions(ctx context.Context, ownerType OwnerType, ownerID string, limit int) ([]Transaction, error)
	CheckDriverStanding(ctx context.Context, driverID string) (DriverStanding, error)
	RequestPayout(ctx context.Context, input PayoutInput) (Wallet, Transaction, error)
	// GetTripSettlement returns how a settled trip's fare was paid. Only the rider
	// and the driver of that trip may see it; anyone else gets ErrSettlementNotFound.
	GetTripSettlement(ctx context.Context, ownerType OwnerType, ownerID string, tripID string) (Settlement, error)
	// RecordTripChange credits the rider's wallet with the change the trip's driver could
	// not return in cash. The platform pays; nothing is taken from the driver.
	RecordTripChange(ctx context.Context, input RecordTripChangeInput) (ChangeCredit, error)
}

type service struct {
	repository Repository
}

func NewService(repository Repository) Service {
	if repository == nil {
		panic("wallet repository is required")
	}

	return &service{repository: repository}
}
