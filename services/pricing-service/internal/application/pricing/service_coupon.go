package pricing

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

func (s *service) CreateCoupon(
	ctx context.Context,
	input CreateCouponInput,
) (Coupon, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if code == "" {
		return Coupon{}, ErrCouponCodeRequired
	}

	switch input.DiscountType {
	case DiscountPercentage:
		if input.DiscountValue.LessThanOrEqual(decimal.Zero) ||
			input.DiscountValue.GreaterThan(decimal.NewFromInt(100)) {
			return Coupon{}, ErrInvalidDiscountValue
		}
	case DiscountFixed:
		if input.DiscountValue.LessThanOrEqual(decimal.Zero) {
			return Coupon{}, ErrInvalidDiscountValue
		}
	default:
		return Coupon{}, ErrInvalidDiscountType
	}

	if !input.ValidUntil.After(input.ValidFrom) {
		return Coupon{}, ErrInvalidValidityWindow
	}

	perRiderLimit := input.PerRiderLimit
	if perRiderLimit <= 0 {
		perRiderLimit = 1
	}

	created, err := s.repository.CreateCoupon(ctx, CreateCouponInput{
		Code:              code,
		DiscountType:      input.DiscountType,
		DiscountValue:     input.DiscountValue,
		ValidFrom:         input.ValidFrom,
		ValidUntil:        input.ValidUntil,
		MaxRedemptions:    input.MaxRedemptions,
		PerRiderLimit:     perRiderLimit,
		MinimumFareAmount: input.MinimumFareAmount,
	})
	if err != nil {
		return Coupon{}, fmt.Errorf("create coupon: %w", err)
	}

	return created, nil
}

func (s *service) GetCoupon(
	ctx context.Context,
	code string,
) (Coupon, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(code))
	if trimmed == "" {
		return Coupon{}, ErrCouponCodeRequired
	}

	found, err := s.repository.FindCouponByCode(ctx, trimmed)
	if err != nil {
		return Coupon{}, fmt.Errorf("get coupon: %w", err)
	}

	return found, nil
}
