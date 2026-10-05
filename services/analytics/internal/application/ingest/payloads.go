package ingest

import "github.com/shopspring/decimal"

// These mirror what each service writes into its outbox payload. Only ids,
// kinds, flags and amounts are read: names, phones and places that some
// payloads carry are never kept.
//
// Decimals: wallet-service writes strings ("15000.500"); pricing-service's
// fare.calculated writes a bare JSON number, so that field is a
// decimal.Decimal (it reads both).

type TripRequestedPayload struct {
	TripID        string `json:"trip_id"`
	RiderID       string `json:"rider_id"`
	CityID        string `json:"city_id"`
	ZoneID        string `json:"zone_id"`
	VehicleClass  string `json:"vehicle_class"`
	PaymentMethod string `json:"payment_method"`
	Scheduled     string `json:"scheduled"`
}

type TripAcceptedPayload struct {
	TripID   string `json:"trip_id"`
	DriverID string `json:"driver_id"`
}

type TripIDPayload struct {
	TripID string `json:"trip_id"`
}

type TripCompletedPayload struct {
	TripID   string `json:"trip_id"`
	RiderID  string `json:"rider_id"`
	DriverID string `json:"driver_id"`
}

type TripCancelledPayload struct {
	TripID        string `json:"trip_id"`
	CancelledBy   string `json:"cancelled_by"`
	RiderNoShow   string `json:"rider_no_show"`
	FromStatus    string `json:"from_status"`
	DriverArrived string `json:"driver_arrived"`
}

type FareCalculatedPayload struct {
	TripID       string          `json:"trip_id"`
	RiderID      string          `json:"rider_id"`
	CurrencyCode string          `json:"currency_code"`
	Total        decimal.Decimal `json:"total"`
	Kind         string          `json:"kind"`
}

type TripSettledPayload struct {
	TripID           string `json:"trip_id"`
	DriverID         string `json:"driver_id"`
	CommissionAmount string `json:"commission_amount"`
}

type RiderCreatedPayload struct {
	RiderID string `json:"rider_id"`
}

type DriverPayload struct {
	DriverID string `json:"driver_id"`
}
