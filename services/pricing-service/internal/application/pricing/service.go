package pricing

import (
	"context"

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
}

type Service interface {
	EstimateFare(ctx context.Context, input EstimateFareInput) (FareBreakdown, error)
	CalculateFare(ctx context.Context, input CalculateFareInput) (Fare, error)
	CreateCoupon(ctx context.Context, input CreateCouponInput) (Coupon, error)
	GetCoupon(ctx context.Context, code string) (Coupon, error)
}

type service struct {
	repository     Repository
	locationClient LocationClient
	routingClient  RoutingClient
	weatherClient  WeatherClient

	// fareRoundingIncrement, when positive, rounds every fare total to a
	// multiple of it (see fare_rounding.go). Zero means no rounding.
	fareRoundingIncrement decimal.Decimal
}

func NewService(
	repository Repository,
	locationClient LocationClient,
	routingClient RoutingClient,
	weatherClient WeatherClient,
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

	s := &service{
		repository:     repository,
		locationClient: locationClient,
		routingClient:  routingClient,
		weatherClient:  weatherClient,
	}

	for _, option := range options {
		option(s)
	}

	return s
}
