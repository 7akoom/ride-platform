package pricing

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type EstimateFareInput struct {
	RiderID      string
	PickupLat    float64
	PickupLng    float64
	DropoffLat   float64
	DropoffLng   float64
	CouponCode   string
	VehicleClass string
}

type CalculateFareInput struct {
	TripID       string
	RiderID      string
	PickupLat    float64
	PickupLng    float64
	DropoffLat   float64
	DropoffLng   float64
	CouponCode   string
	VehicleClass string
	// QuoteID, when set, is the quote the trip was requested with: the fare
	// is the quoted one and the fields above only identify the trip.
	QuoteID string
}

type QuoteTripInput struct {
	RiderID    string
	PickupLat  float64
	PickupLng  float64
	DropoffLat float64
	DropoffLng float64
	CouponCode string
}

// TripQuotes is the answer to QuoteTrip: one quote per vehicle class,
// cheapest first.
type TripQuotes struct {
	ZoneID string
	CityID string
	Quotes []Quote
}

type Service interface {
	EstimateFare(ctx context.Context, input EstimateFareInput) (FareBreakdown, error)
	CalculateFare(ctx context.Context, input CalculateFareInput) (Fare, error)
	CreateCoupon(ctx context.Context, input CreateCouponInput) (Coupon, error)
	GetCoupon(ctx context.Context, code string) (Coupon, error)

	// QuoteTrip prices a trip for every vehicle class and holds each price
	// for the quote TTL.
	QuoteTrip(ctx context.Context, input QuoteTripInput) (TripQuotes, error)
	// ClaimQuote gives a quote to the trip being requested with it.
	ClaimQuote(ctx context.Context, quoteID, riderID, tripID string) (Quote, error)
	// ReleaseQuote frees a quote whose trip was never created.
	ReleaseQuote(ctx context.Context, quoteID, tripID string) error
	// DeleteUnclaimedQuotes removes quotes that expired a while ago without
	// becoming a trip.
	DeleteUnclaimedQuotes(ctx context.Context) (int, error)
}

// DefaultQuoteTTL is how long a quote holds its price.
const DefaultQuoteTTL = 5 * time.Minute

// unclaimedQuoteRetention is how long an expired quote nobody claimed is
// kept before DeleteUnclaimedQuotes removes it. Long enough to cover the
// demand window (see demandWindow) with plenty to spare.
const unclaimedQuoteRetention = 24 * time.Hour

type service struct {
	repository     Repository
	locationClient LocationClient
	routingClient  RoutingClient
	weatherClient  WeatherClient
	driverFinder   DriverFinder

	// fareRoundingIncrement, when positive, rounds every fare total to a
	// multiple of it (see fare_rounding.go). Zero means no rounding.
	fareRoundingIncrement decimal.Decimal

	quoteTTL time.Duration
}

func NewService(
	repository Repository,
	locationClient LocationClient,
	routingClient RoutingClient,
	weatherClient WeatherClient,
	driverFinder DriverFinder,
	options ...Option,
) Service {
	if repository == nil {
		panic("pricing repository is required")
	}

	if locationClient == nil {
		panic("location client is required")
	}

	if routingClient == nil {
		panic("routing client is required")
	}

	if weatherClient == nil {
		panic("weather client is required")
	}

	if driverFinder == nil {
		panic("driver finder is required")
	}

	s := &service{
		repository:     repository,
		locationClient: locationClient,
		routingClient:  routingClient,
		weatherClient:  weatherClient,
		driverFinder:   driverFinder,
		quoteTTL:       DefaultQuoteTTL,
	}

	for _, option := range options {
		option(s)
	}

	return s
}
