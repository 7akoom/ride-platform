package pricing

import (
	"time"

	"github.com/shopspring/decimal"
)

// Option customises a service at construction time.
type Option func(*service)

// WithFareRounding makes every fare total a whole multiple of increment
// (250 for the Iraqi dinar, whose smallest banknote is 250). A zero
// increment, the default, leaves totals unrounded.
func WithFareRounding(increment decimal.Decimal) Option {
	return func(s *service) {
		s.fareRoundingIncrement = increment
	}
}

// WithQuoteTTL sets how long a quote holds its price (DefaultQuoteTTL when
// not positive).
func WithQuoteTTL(ttl time.Duration) Option {
	return func(s *service) {
		if ttl > 0 {
			s.quoteTTL = ttl
		}
	}
}

// roundToIncrement rounds amount to the nearest multiple of increment,
// with exact halves rounding up. It is applied ONCE, to the final fare
// total, so the estimate, the stored fare, the published fare.calculated
// event and the wallet settlement all carry exactly the same number.
//
// A non-positive increment leaves the amount untouched. A positive fare
// never rounds down to zero: the smallest charge is one increment, so a
// very short trip is not free.
func roundToIncrement(amount, increment decimal.Decimal) decimal.Decimal {
	if !increment.IsPositive() || !amount.IsPositive() {
		return amount
	}

	rounded := amount.Div(increment).Round(0).Mul(increment)
	if rounded.IsZero() {
		return increment
	}

	return rounded
}
