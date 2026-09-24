package promotions

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// Limits on what staff can set. Money fits NUMERIC(12, 4), percents
// NUMERIC(5, 2).
var (
	maxMoney   = decimal.NewFromInt(99_999_999)
	maxPercent = decimal.NewFromInt(100)
)

const (
	maxDescriptionLength = 200
	maxRedemptionsLimit  = 10_000_000
	maxPerRiderLimit     = 1000
	maxLoyaltyEvery      = 100

	defaultPageSize = 50
	maxPageSize     = 100
)

// codePattern: 3-40 letters, digits, "-" and "_", starting with a letter or
// a digit (after upper-casing).
var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{2,39}$`)

// Service is what the staff endpoints call.
type Service struct {
	repository Repository
	places     Places
	now        func() time.Time
}

func NewService(repository Repository, places Places) *Service {
	if repository == nil {
		panic("promotions repository is required")
	}

	if places == nil {
		panic("places are required")
	}

	return &Service{repository: repository, places: places, now: func() time.Time { return time.Now().UTC() }}
}

// --- coupons -----------------------------------------------------------------

func (s *Service) CreateCoupon(ctx context.Context, input CouponInput, staffID string) (pricing.Coupon, error) {
	code := pricing.NormalizeCouponCode(input.Code)
	if !codePattern.MatchString(code) {
		return pricing.Coupon{}, invalid("code", "must be 3-40 letters, digits, '-' or '_'")
	}

	description := strings.TrimSpace(input.Description)
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		return pricing.Coupon{}, invalid("description", "must be at most 200 characters")
	}

	var maxDiscount *decimal.Decimal

	switch input.DiscountType {
	case pricing.DiscountPercentage:
		if err := checkPercent("discount_value", input.DiscountValue, false); err != nil {
			return pricing.Coupon{}, err
		}

		if input.MaxDiscountAmount != nil {
			if err := checkPositiveMoney("max_discount_amount", *input.MaxDiscountAmount); err != nil {
				return pricing.Coupon{}, err
			}

			maxDiscount = input.MaxDiscountAmount
		}
	case pricing.DiscountFixed:
		if err := checkPositiveMoney("discount_value", input.DiscountValue); err != nil {
			return pricing.Coupon{}, err
		}

		if input.MaxDiscountAmount != nil {
			return pricing.Coupon{}, invalid("max_discount_amount", "is only for a percentage")
		}
	default:
		return pricing.Coupon{}, invalid("discount_type", "must be a percentage or a fixed amount")
	}

	now := s.now()

	validFrom := input.ValidFrom
	if validFrom.IsZero() {
		validFrom = now
	}

	switch {
	case input.ValidUntil.IsZero():
		return pricing.Coupon{}, invalid("valid_until", "is required")
	case !input.ValidUntil.After(validFrom):
		return pricing.Coupon{}, invalid("valid_until", "must be after valid_from")
	case !input.ValidUntil.After(now):
		return pricing.Coupon{}, invalid("valid_until", "must be in the future")
	}

	if input.MaxRedemptions < 0 || input.MaxRedemptions > maxRedemptionsLimit {
		return pricing.Coupon{}, invalid("max_redemptions", "must be between 0 (unlimited) and 10000000")
	}

	perRider := input.PerRiderLimit
	if perRider == 0 {
		perRider = 1
	}

	if perRider < 1 || perRider > maxPerRiderLimit {
		return pricing.Coupon{}, invalid("per_rider_limit", "must be between 1 and 1000")
	}

	if err := checkMoney("minimum_fare_amount", input.MinimumFareAmount); err != nil {
		return pricing.Coupon{}, err
	}

	cityID, zoneID, err := s.checkPlace(ctx, input.CityID, input.ZoneID)
	if err != nil {
		return pricing.Coupon{}, err
	}

	classes, err := checkClasses(input.VehicleClasses)
	if err != nil {
		return pricing.Coupon{}, err
	}

	var maxRedemptions *int
	if input.MaxRedemptions > 0 {
		value := input.MaxRedemptions
		maxRedemptions = &value
	}

	return s.repository.CreateCoupon(ctx, pricing.Coupon{
		Code:              code,
		Description:       description,
		DiscountType:      input.DiscountType,
		DiscountValue:     input.DiscountValue,
		MaxDiscountAmount: maxDiscount,
		ValidFrom:         validFrom,
		ValidUntil:        input.ValidUntil,
		MaxRedemptions:    maxRedemptions,
		PerRiderLimit:     perRider,
		MinimumFareAmount: input.MinimumFareAmount,
		CityID:            cityID,
		ZoneID:            zoneID,
		VehicleClasses:    classes,
		NewRidersOnly:     input.NewRidersOnly,
		Active:            true,
		CreatedBy:         staffID,
		UpdatedBy:         staffID,
	})
}

func (s *Service) GetCoupon(ctx context.Context, code string) (CouponDetails, error) {
	code = pricing.NormalizeCouponCode(code)
	if !codePattern.MatchString(code) {
		return CouponDetails{}, pricing.ErrCouponNotFound
	}

	return s.repository.FindCouponDetails(ctx, code)
}

// ListCoupons pages the coupons, newest first. pageToken is the previous
// page's next token (empty for the first).
func (s *Service) ListCoupons(ctx context.Context, state, query string, pageSize int, pageToken string) (Page[pricing.Coupon], error) {
	state = strings.ToLower(strings.TrimSpace(state))

	switch state {
	case "", StateRunning, StateScheduled, StateFinished:
	default:
		return Page[pricing.Coupon]{}, invalid("state", "must be running, scheduled or finished")
	}

	offset, limit, err := paging(pageSize, pageToken)
	if err != nil {
		return Page[pricing.Coupon]{}, err
	}

	coupons, err := s.repository.ListCoupons(ctx, CouponFilter{
		State:  state,
		Prefix: pricing.NormalizeCouponCode(query),
		Now:    s.now(),
		Offset: offset,
		Limit:  limit + 1,
	})
	if err != nil {
		return Page[pricing.Coupon]{}, err
	}

	return pageOf(coupons, offset, limit), nil
}

// UpdateCoupon changes what may change once riders use a coupon.
func (s *Service) UpdateCoupon(ctx context.Context, code string, change CouponChange, staffID string) (pricing.Coupon, error) {
	code = pricing.NormalizeCouponCode(code)
	if !codePattern.MatchString(code) {
		return pricing.Coupon{}, pricing.ErrCouponNotFound
	}

	current, err := s.repository.FindCouponDetails(ctx, code)
	if err != nil {
		return pricing.Coupon{}, err
	}

	if change.Description != nil {
		description := strings.TrimSpace(*change.Description)
		if utf8.RuneCountInString(description) > maxDescriptionLength {
			return pricing.Coupon{}, invalid("description", "must be at most 200 characters")
		}

		change.Description = &description
	}

	if change.ValidUntil != nil && !change.ValidUntil.After(current.Coupon.ValidFrom) {
		return pricing.Coupon{}, invalid("valid_until", "must be after valid_from")
	}

	if change.MaxRedemptions != nil && (*change.MaxRedemptions < 0 || *change.MaxRedemptions > maxRedemptionsLimit) {
		return pricing.Coupon{}, invalid("max_redemptions", "must be between 0 (unlimited) and 10000000")
	}

	if change.PerRiderLimit != nil && (*change.PerRiderLimit < 1 || *change.PerRiderLimit > maxPerRiderLimit) {
		return pricing.Coupon{}, invalid("per_rider_limit", "must be between 1 and 1000")
	}

	if change.MinimumFareAmount != nil {
		if err := checkMoney("minimum_fare_amount", *change.MinimumFareAmount); err != nil {
			return pricing.Coupon{}, err
		}
	}

	return s.repository.UpdateCoupon(ctx, code, change, staffID)
}

// ListRedemptions pages a coupon's use, newest first.
func (s *Service) ListRedemptions(ctx context.Context, code string, pageSize int, pageToken string) (Page[Redemption], error) {
	details, err := s.GetCoupon(ctx, code)
	if err != nil {
		return Page[Redemption]{}, err
	}

	offset, limit, err := paging(pageSize, pageToken)
	if err != nil {
		return Page[Redemption]{}, err
	}

	redemptions, err := s.repository.ListRedemptions(ctx, details.Coupon.ID, offset, limit+1)
	if err != nil {
		return Page[Redemption]{}, err
	}

	return pageOf(redemptions, offset, limit), nil
}

// --- automatic discounts ---------------------------------------------------------

func (s *Service) GetSettings(ctx context.Context) (pricing.PromotionSettings, error) {
	return s.repository.GetPromotionSettings(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, input SettingsInput, staffID string) (pricing.PromotionSettings, error) {
	if err := checkPercent("first_ride_percent", input.FirstRidePercent, true); err != nil {
		return pricing.PromotionSettings{}, err
	}

	if err := checkPercent("loyalty_percent", input.LoyaltyPercent, true); err != nil {
		return pricing.PromotionSettings{}, err
	}

	if input.LoyaltyEvery != 0 && (input.LoyaltyEvery < 2 || input.LoyaltyEvery > maxLoyaltyEvery) {
		return pricing.PromotionSettings{}, invalid("loyalty_every", "must be 0 (off) or between 2 and 100")
	}

	for _, limit := range []struct {
		field  string
		amount *decimal.Decimal
	}{
		{"first_ride_max_amount", input.FirstRideMaxAmount},
		{"loyalty_max_amount", input.LoyaltyMaxAmount},
	} {
		if limit.amount == nil {
			continue
		}

		if err := checkPositiveMoney(limit.field, *limit.amount); err != nil {
			return pricing.PromotionSettings{}, err
		}
	}

	return s.repository.SetPromotionSettings(ctx, pricing.PromotionSettings{
		FirstRidePercent:   input.FirstRidePercent,
		FirstRideMaxAmount: input.FirstRideMaxAmount,
		LoyaltyEvery:       input.LoyaltyEvery,
		LoyaltyPercent:     input.LoyaltyPercent,
		LoyaltyMaxAmount:   input.LoyaltyMaxAmount,
		UpdatedBy:          staffID,
	})
}

// --- checks ------------------------------------------------------------------------

func (s *Service) checkPlace(ctx context.Context, cityID, zoneID string) (string, string, error) {
	cityID = strings.TrimSpace(cityID)
	zoneID = strings.TrimSpace(zoneID)

	switch {
	case cityID != "" && zoneID != "":
		return "", "", invalid("zone_id", "a coupon is for a zone or a city, not both")

	case zoneID != "":
		if !looksLikeUUID(zoneID) {
			return "", "", ErrZoneNotFound
		}

		exists, err := s.places.ZoneExists(ctx, zoneID)
		if err != nil {
			return "", "", fmt.Errorf("%w: %v", ErrPlacesUnavailable, err)
		}

		if !exists {
			return "", "", ErrZoneNotFound
		}

	case cityID != "":
		if !looksLikeUUID(cityID) {
			return "", "", ErrCityNotFound
		}

		exists, err := s.places.CityExists(ctx, cityID)
		if err != nil {
			return "", "", fmt.Errorf("%w: %v", ErrPlacesUnavailable, err)
		}

		if !exists {
			return "", "", ErrCityNotFound
		}
	}

	return cityID, zoneID, nil
}

// checkClasses normalizes the classes, sorted and without repeats; empty
// means every class.
func checkClasses(raw []string) ([]string, error) {
	seen := map[string]struct{}{}
	classes := []string{}

	for _, value := range raw {
		if strings.TrimSpace(value) == "" {
			return nil, invalid("vehicle_classes", "must not contain an empty class")
		}

		class, err := pricing.NormalizeVehicleClass(value)
		if err != nil {
			return nil, invalid("vehicle_classes", "must be "+strings.Join(pricing.VehicleClasses(), " or "))
		}

		if _, dup := seen[class]; !dup {
			seen[class] = struct{}{}
			classes = append(classes, class)
		}
	}

	sort.Strings(classes)

	return classes, nil
}

func checkMoney(field string, amount decimal.Decimal) error {
	switch {
	case amount.IsNegative():
		return invalid(field, "must not be negative")
	case amount.GreaterThan(maxMoney):
		return invalid(field, "is too large")
	case !amount.Equal(amount.Round(4)):
		return invalid(field, "has more than 4 decimal places")
	}

	return nil
}

func checkPositiveMoney(field string, amount decimal.Decimal) error {
	if !amount.IsPositive() {
		return invalid(field, "must be more than 0")
	}

	return checkMoney(field, amount)
}

// checkPercent accepts 0 (when zeroAllowed) up to 100, with at most two
// decimal places.
func checkPercent(field string, percent decimal.Decimal, zeroAllowed bool) error {
	switch {
	case percent.IsNegative(), !zeroAllowed && percent.IsZero():
		if zeroAllowed {
			return invalid(field, "must be between 0 and 100")
		}

		return invalid(field, "must be more than 0 and at most 100")
	case percent.GreaterThan(maxPercent):
		return invalid(field, "must be at most 100")
	case !percent.Equal(percent.Round(2)):
		return invalid(field, "has more than 2 decimal places")
	}

	return nil
}

// paging turns a page size and token into an offset and limit. The token
// is the offset of the page's first item.
func paging(pageSize int, pageToken string) (int, int, error) {
	limit := pageSize

	switch {
	case limit <= 0:
		limit = defaultPageSize
	case limit > maxPageSize:
		limit = maxPageSize
	}

	pageToken = strings.TrimSpace(pageToken)
	if pageToken == "" {
		return 0, limit, nil
	}

	var offset int
	if _, err := fmt.Sscanf(pageToken, "%d", &offset); err != nil || offset < 0 || fmt.Sprint(offset) != pageToken {
		return 0, 0, invalid("page_token", "is not a token from a previous page")
	}

	return offset, limit, nil
}

// pageOf cuts the one extra item a listing asked for, which says whether
// there is a next page.
func pageOf[T any](items []T, offset, limit int) Page[T] {
	if len(items) > limit {
		return Page[T]{Items: items[:limit], NextOffset: offset + limit}
	}

	return Page[T]{Items: items}
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}

	return true
}
