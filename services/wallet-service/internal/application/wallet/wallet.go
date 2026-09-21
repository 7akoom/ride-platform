package wallet

import (
	"time"

	"github.com/shopspring/decimal"
)

// Money is exact decimal arithmetic, not float64. A wallet balance is
// added to and subtracted from thousands of times over its life; with
// binary floating point those operations accumulate error until the
// ledger no longer sums to the balance. decimal.Decimal has no such
// drift, and it stores "2451.6" as exactly that — no minor-units
// conversion needed for display.
type Money = decimal.Decimal

type OwnerType string

const (
	OwnerRider  OwnerType = "rider"
	OwnerDriver OwnerType = "driver"
)

func (o OwnerType) Valid() bool {
	return o == OwnerRider || o == OwnerDriver
}

type PaymentMethod string

const (
	PaymentCash   PaymentMethod = "cash"
	PaymentWallet PaymentMethod = "wallet"
	PaymentCard   PaymentMethod = "card"
)

func (p PaymentMethod) Valid() bool {
	switch p {
	case PaymentCash, PaymentWallet, PaymentCard:
		return true
	default:
		return false
	}
}

type TransactionType string

const (
	TxTopUp       TransactionType = "top_up"
	TxTripPayment TransactionType = "trip_payment"
	TxTripEarning TransactionType = "trip_earning"
	TxCommission  TransactionType = "commission"
	TxPayout      TransactionType = "payout"
	TxAdjustment  TransactionType = "adjustment"
)

type Wallet struct {
	ID           string
	OwnerType    OwnerType
	OwnerID      string
	CurrencyCode string
	Balance      Money
	Blocked      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Transaction struct {
	ID           string
	WalletID     string
	Type         TransactionType
	Amount       Money // signed: negative means money left the wallet
	BalanceAfter Money
	TripID       string
	Description  string
	CreatedAt    time.Time
}

// Config is the active commission/limits card for this deployment.
type Config struct {
	ID                  string
	CurrencyCode        string
	CommissionRate      Money // percentage, e.g. 20 means 20%
	SuspensionThreshold Money // stored positive; account suspends at balance <= -SuspensionThreshold
	MinimumPayoutAmount Money
	// The most change one trip may credit to a rider's wallet (see RecordTripChange).
	MaxChangeCredit Money
	CreatedAt       time.Time
}

// Settlement is the record of how one trip's money was split.
type Settlement struct {
	TripID           string
	RiderID          string
	DriverID         string
	CurrencyCode     string
	PaymentMethod    PaymentMethod
	FareAmount       Money
	CommissionRate   Money
	CommissionAmount Money
	DriverEarning    Money
	RiderBalance     Money
	DriverBalance    Money
	// How much of the fare left the rider's wallet and how much the rider handed
	// to the driver in cash. WalletAmount + CashAmount is the fare for a wallet or
	// cash trip; a card trip moves neither.
	WalletAmount Money
	CashAmount   Money
	// The change credited to the rider's wallet because the driver had none (zero if none).
	ChangeAmount Money
}

// CommissionFor returns the platform's cut of a fare, rounded to the
// currency's scale. Rounding happens once, here, so commission and
// driver earning always sum back to exactly the fare.
func CommissionFor(fare, ratePercent Money) Money {
	return fare.Mul(ratePercent).Div(decimal.NewFromInt(100)).Round(moneyScale)
}

// SuspensionFloor is the balance at or below which a driver account is
// suspended. Config stores the threshold positive for readability; this
// converts it to the actual signed floor.
func (c Config) SuspensionFloor() Money {
	return c.SuspensionThreshold.Neg()
}

// IsSuspendedAt reports whether a given balance puts a driver out of
// good standing.
func (c Config) IsSuspendedAt(balance Money) bool {
	return balance.LessThanOrEqual(c.SuspensionFloor())
}

// AmountDueToReactivate is what the driver must deposit to get back
// above the suspension floor. Zero when they are already in good
// standing.
//
// Rounded UP to a whole currency unit. Strictly, clearing the floor
// needs one more minor unit than the exact shortfall — but showing a
// driver "deposit 10000.001" is useless, and rounding down would leave
// them one fraction short and still suspended. Up to the whole unit is
// the only version that's both correct and presentable.
func (c Config) AmountDueToReactivate(balance Money) Money {
	if !c.IsSuspendedAt(balance) {
		return decimal.Zero
	}

	shortfall := c.SuspensionFloor().Sub(balance)

	return shortfall.Add(decimal.New(1, -moneyScale)).Ceil()
}

// moneyScale matches the NUMERIC(16,3) columns — three decimal places
// covers every currency this platform is likely to be deployed in.
const moneyScale = 3
