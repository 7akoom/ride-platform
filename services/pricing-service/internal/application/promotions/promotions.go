// Package promotions is how staff run discounts: coupons riders enter when
// asking for quotes, and the automatic first-ride and loyalty discounts.
// The fare pipeline that applies them lives in package pricing.
package promotions

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

var (
	ErrCityNotFound      = errors.New("city not found")
	ErrZoneNotFound      = errors.New("zone not found")
	ErrPlacesUnavailable = errors.New("cities and zones could not be checked")
)

// InvalidError names the field a request got wrong and why.
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Reason }

func invalid(field, reason string) error { return &InvalidError{Field: field, Reason: reason} }

// Places checks cities and zones exist (location-service). Inactive ones
// count: a coupon may be made for a city before it opens.
type Places interface {
	CityExists(ctx context.Context, cityID string) (bool, error)
	ZoneExists(ctx context.Context, zoneID string) (bool, error)
}

// Coupon listing states.
const (
	StateRunning   = "running"
	StateScheduled = "scheduled"
	// StateFinished is expired, used up or turned off.
	StateFinished = "finished"
)

// CouponFilter narrows and pages a listing, newest first.
type CouponFilter struct {
	State  string
	Prefix string
	Now    time.Time
	Offset int
	Limit  int
}

// CouponDetails is a coupon with what it has been used for.
type CouponDetails struct {
	Coupon pricing.Coupon
	// RedeemedCount is how many completed trips used it, DiscountGiven the
	// discount they got in total.
	RedeemedCount int
	DiscountGiven decimal.Decimal
}

// CouponChange is what UpdateCoupon may change; nil fields stay.
// MaxRedemptions 0 means unlimited.
type CouponChange struct {
	Description       *string
	ValidUntil        *time.Time
	MaxRedemptions    *int
	PerRiderLimit     *int
	MinimumFareAmount *decimal.Decimal
	Active            *bool
}

// Redemption is one trip's hold on a coupon.
type Redemption struct {
	ID             string
	RiderID        string
	TripID         string
	QuoteID        string
	Status         string
	DiscountAmount decimal.Decimal
	CreatedAt      time.Time
	ReleasedAt     *time.Time
}

// Repository stores coupons and the automatic discounts.
type Repository interface {
	// CreateCoupon returns pricing.ErrCouponAlreadyExists for a taken code.
	CreateCoupon(ctx context.Context, coupon pricing.Coupon) (pricing.Coupon, error)
	// FindCouponDetails returns pricing.ErrCouponNotFound for an unknown code.
	FindCouponDetails(ctx context.Context, code string) (CouponDetails, error)
	ListCoupons(ctx context.Context, filter CouponFilter) ([]pricing.Coupon, error)
	// UpdateCoupon returns pricing.ErrCouponNotFound for an unknown code.
	UpdateCoupon(ctx context.Context, code string, change CouponChange, staffID string) (pricing.Coupon, error)
	// ListRedemptions returns a coupon's holds, newest first.
	ListRedemptions(ctx context.Context, couponID string, offset, limit int) ([]Redemption, error)

	GetPromotionSettings(ctx context.Context) (pricing.PromotionSettings, error)
	SetPromotionSettings(ctx context.Context, settings pricing.PromotionSettings) (pricing.PromotionSettings, error)
}

// CouponInput is a new coupon. ValidFrom zero means now; MaxRedemptions 0
// means unlimited and PerRiderLimit 0 means 1. VehicleClasses empty means
// every class.
type CouponInput struct {
	Code              string
	Description       string
	DiscountType      pricing.DiscountType
	DiscountValue     decimal.Decimal
	MaxDiscountAmount *decimal.Decimal
	ValidFrom         time.Time
	ValidUntil        time.Time
	MaxRedemptions    int
	PerRiderLimit     int
	MinimumFareAmount decimal.Decimal
	CityID            string
	ZoneID            string
	VehicleClasses    []string
	NewRidersOnly     bool
}

// SettingsInput replaces the automatic discounts.
type SettingsInput struct {
	FirstRidePercent   decimal.Decimal
	FirstRideMaxAmount *decimal.Decimal
	LoyaltyEvery       int
	LoyaltyPercent     decimal.Decimal
	LoyaltyMaxAmount   *decimal.Decimal
}

// Page is one page of a listing: NextOffset is where the next one starts,
// 0 when there is none.
type Page[T any] struct {
	Items      []T
	NextOffset int
}
