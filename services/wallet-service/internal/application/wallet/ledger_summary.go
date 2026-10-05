package wallet

import (
	"time"

	"github.com/shopspring/decimal"
)

// LedgerTotal is one day's movements of one type into or out of one kind of
// wallet.
type LedgerTotal struct {
	Date      time.Time
	OwnerType string
	Type      string
	Entries   int64
	Credited  decimal.Decimal
	Debited   decimal.Decimal
}

// LedgerSummary is the ledger by day and what wallets hold now.
type LedgerSummary struct {
	Totals           []LedgerTotal
	CurrencyCode     string
	RiderBalances    decimal.Decimal
	DriverCredit     decimal.Decimal
	DriverDebt       decimal.Decimal
	SuspendedDrivers int64
	RiderDues        decimal.Decimal
}
