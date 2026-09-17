package ingest

import "github.com/shopspring/decimal"

// These structs mirror exactly what each service's outbox writer marshals
// into the payload field (see each service's *_repository.go). Field names
// and presence/absence are copied verbatim — e.g. TripSettledPayload has no
// currency, because wallet-service's trip.settled payload genuinely omits
// it (see the trip_fare_currency lookup this package relies on instead).
//
// IMPORTANT: not every service serializes decimal.Decimal the same way.
// wallet-service calls .String() explicitly, producing a quoted JSON string
// ("15000.500"). pricing-service's fare.calculated instead marshals the
// decimal.Decimal value directly through `any`, which shopspring/decimal
// encodes as a bare JSON number (15000.5, no quotes) — so that one field
// must be typed decimal.Decimal here (its UnmarshalJSON accepts both forms),
// never plain string.

type TripRequestedPayload struct {
	TripID  string `json:"trip_id"`
	RiderID string `json:"rider_id"`
}

type TripAcceptedPayload struct {
	TripID   string `json:"trip_id"`
	DriverID string `json:"driver_id"`
}

type TripStartedPayload struct {
	TripID string `json:"trip_id"`
}

type TripCompletedPayload struct {
	TripID   string `json:"trip_id"`
	RiderID  string `json:"rider_id"`
	DriverID string `json:"driver_id"`
}

type TripCancelledPayload struct {
	TripID string `json:"trip_id"`
	Reason string `json:"reason"`
}

type FareCalculatedPayload struct {
	TripID       string          `json:"trip_id"`
	RiderID      string          `json:"rider_id"`
	CurrencyCode string          `json:"currency_code"`
	Total        decimal.Decimal `json:"total"` // bare JSON number, not a string
}

type TripSettledPayload struct {
	TripID           string `json:"trip_id"`
	RiderID          string `json:"rider_id"`
	DriverID         string `json:"driver_id"`
	PaymentMethod    string `json:"payment_method"`
	FareAmount       string `json:"fare_amount"`       // decimal string (wallet-service calls .String())
	CommissionAmount string `json:"commission_amount"` // decimal string
	DriverEarning    string `json:"driver_earning"`    // decimal string
}

type RiderCreatedPayload struct {
	RiderID     string `json:"rider_id"`
	IdentityID  string `json:"identity_id"`
	DisplayName string `json:"display_name"`
}

type DriverCreatedPayload struct {
	DriverID    string `json:"driver_id"`
	IdentityID  string `json:"identity_id"`
	DisplayName string `json:"display_name"`
}
