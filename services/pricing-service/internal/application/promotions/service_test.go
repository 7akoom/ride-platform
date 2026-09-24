package promotions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const (
	cityID  = "11111111-1111-4111-8111-111111111111"
	zoneID  = "22222222-2222-4222-8222-222222222222"
	staffID = "33333333-3333-4333-8333-333333333333"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type fakeRepository struct {
	created  []pricing.Coupon
	coupons  map[string]pricing.Coupon
	filter   CouponFilter
	listed   []pricing.Coupon
	changes  []CouponChange
	settings pricing.PromotionSettings
	redeemed []Redemption
	offsets  [2]int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{coupons: map[string]pricing.Coupon{}}
}

func (r *fakeRepository) CreateCoupon(_ context.Context, c pricing.Coupon) (pricing.Coupon, error) {
	if _, taken := r.coupons[c.Code]; taken {
		return pricing.Coupon{}, pricing.ErrCouponAlreadyExists
	}

	c.ID = "coupon-" + c.Code
	r.coupons[c.Code] = c
	r.created = append(r.created, c)

	return c, nil
}

func (r *fakeRepository) FindCouponDetails(_ context.Context, code string) (CouponDetails, error) {
	c, ok := r.coupons[code]
	if !ok {
		return CouponDetails{}, pricing.ErrCouponNotFound
	}

	return CouponDetails{Coupon: c}, nil
}

func (r *fakeRepository) ListCoupons(_ context.Context, filter CouponFilter) ([]pricing.Coupon, error) {
	r.filter = filter

	return r.listed, nil
}

func (r *fakeRepository) UpdateCoupon(_ context.Context, code string, change CouponChange, _ string) (pricing.Coupon, error) {
	r.changes = append(r.changes, change)

	return r.coupons[code], nil
}

func (r *fakeRepository) ListRedemptions(_ context.Context, _ string, offset, limit int) ([]Redemption, error) {
	r.offsets = [2]int{offset, limit}

	return r.redeemed, nil
}

func (r *fakeRepository) GetPromotionSettings(_ context.Context) (pricing.PromotionSettings, error) {
	return r.settings, nil
}

func (r *fakeRepository) SetPromotionSettings(_ context.Context, s pricing.PromotionSettings) (pricing.PromotionSettings, error) {
	r.settings = s

	return s, nil
}

type fakePlaces struct {
	err error
}

func (p fakePlaces) CityExists(_ context.Context, id string) (bool, error) {
	return id == cityID, p.err
}
func (p fakePlaces) ZoneExists(_ context.Context, id string) (bool, error) {
	return id == zoneID, p.err
}

func newTestService() (*Service, *fakeRepository) {
	repo := newFakeRepository()
	service := NewService(repo, fakePlaces{})
	service.now = func() time.Time { return now }

	return service, repo
}

func dec(v string) decimal.Decimal { return decimal.RequireFromString(v) }

func ptr[T any](v T) *T { return &v }

func validCoupon() CouponInput {
	return CouponInput{
		Code:          " welcome-10 ",
		DiscountType:  pricing.DiscountPercentage,
		DiscountValue: dec("10"),
		ValidUntil:    now.Add(30 * 24 * time.Hour),
	}
}

func TestCreatingACoupon(t *testing.T) {
	service, repo := newTestService()

	input := validCoupon()
	input.VehicleClasses = []string{"Comfort", "economy", "comfort"}
	input.CityID = cityID
	input.MaxDiscountAmount = ptr(dec("2000"))

	created, err := service.CreateCoupon(context.Background(), input, staffID)
	if err != nil {
		t.Fatal(err)
	}

	if created.Code != "WELCOME-10" || created.PerRiderLimit != 1 || created.MaxRedemptions != nil ||
		!created.ValidFrom.Equal(now) || !created.Active || created.CreatedBy != staffID || created.CityID != cityID ||
		len(created.VehicleClasses) != 2 || created.VehicleClasses[0] != "comfort" || created.MaxDiscountAmount == nil {
		t.Fatalf("created %+v", created)
	}

	if _, err := service.CreateCoupon(context.Background(), validCoupon(), staffID); !errors.Is(err, pricing.ErrCouponAlreadyExists) {
		t.Fatalf("a taken code: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("created %d", len(repo.created))
	}
}

func TestACouponIsChecked(t *testing.T) {
	cases := []struct {
		name   string
		change func(*CouponInput)
		field  string
		err    error
	}{
		{"a short code", func(c *CouponInput) { c.Code = "ab" }, "code", nil},
		{"a code with a space", func(c *CouponInput) { c.Code = "SAVE 10" }, "code", nil},
		{"a code starting with a dash", func(c *CouponInput) { c.Code = "-SAVE" }, "code", nil},
		{"no discount type", func(c *CouponInput) { c.DiscountType = pricing.DiscountNone }, "discount_type", nil},
		{"a percentage over 100", func(c *CouponInput) { c.DiscountValue = dec("100.5") }, "discount_value", nil},
		{"a percentage of 0", func(c *CouponInput) { c.DiscountValue = dec("0") }, "discount_value", nil},
		{"three decimals of a percent", func(c *CouponInput) { c.DiscountValue = dec("10.125") }, "discount_value", nil},
		{"a fixed amount of 0", func(c *CouponInput) { c.DiscountType = pricing.DiscountFixed; c.DiscountValue = dec("0") }, "discount_value", nil},
		{"a cap on a fixed amount", func(c *CouponInput) {
			c.DiscountType = pricing.DiscountFixed
			c.DiscountValue = dec("500")
			c.MaxDiscountAmount = ptr(dec("400"))
		}, "max_discount_amount", nil},
		{"a cap of 0", func(c *CouponInput) { c.MaxDiscountAmount = ptr(dec("0")) }, "max_discount_amount", nil},
		{"no end", func(c *CouponInput) { c.ValidUntil = time.Time{} }, "valid_until", nil},
		{"an end before the start", func(c *CouponInput) { c.ValidFrom = now.Add(48 * time.Hour); c.ValidUntil = now.Add(24 * time.Hour) }, "valid_until", nil},
		{"an end in the past", func(c *CouponInput) { c.ValidFrom = now.Add(-48 * time.Hour); c.ValidUntil = now.Add(-time.Hour) }, "valid_until", nil},
		{"negative uses", func(c *CouponInput) { c.MaxRedemptions = -1 }, "max_redemptions", nil},
		{"too many uses per rider", func(c *CouponInput) { c.PerRiderLimit = 1001 }, "per_rider_limit", nil},
		{"a negative minimum", func(c *CouponInput) { c.MinimumFareAmount = dec("-1") }, "minimum_fare_amount", nil},
		{"a zone and a city", func(c *CouponInput) { c.ZoneID = zoneID; c.CityID = cityID }, "zone_id", nil},
		{"an unknown city", func(c *CouponInput) { c.CityID = zoneID }, "", ErrCityNotFound},
		{"an unknown zone", func(c *CouponInput) { c.ZoneID = "zone" }, "", ErrZoneNotFound},
		{"an unknown class", func(c *CouponInput) { c.VehicleClasses = []string{"van"} }, "vehicle_classes", nil},
		{"an empty class", func(c *CouponInput) { c.VehicleClasses = []string{" "} }, "vehicle_classes", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repo := newTestService()
			input := validCoupon()
			tc.change(&input)

			_, err := service.CreateCoupon(context.Background(), input, staffID)

			var invalidErr *InvalidError

			switch {
			case tc.err != nil && !errors.Is(err, tc.err):
				t.Fatalf("got %v, want %v", err, tc.err)
			case tc.err == nil && (!errors.As(err, &invalidErr) || invalidErr.Field != tc.field):
				t.Fatalf("got %v, want a problem with %s", err, tc.field)
			}

			if len(repo.created) != 0 {
				t.Fatal("nothing may be created")
			}
		})
	}
}

func TestPlacesThatCannotBeCheckedFailTheCoupon(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, fakePlaces{err: errors.New("location-service down")})
	service.now = func() time.Time { return now }

	input := validCoupon()
	input.CityID = cityID

	if _, err := service.CreateCoupon(context.Background(), input, staffID); !errors.Is(err, ErrPlacesUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestListingCouponsIsPaged(t *testing.T) {
	service, repo := newTestService()
	repo.listed = []pricing.Coupon{{Code: "A"}, {Code: "B"}, {Code: "C"}}

	page, err := service.ListCoupons(context.Background(), "Running", " wel", 2, "")
	if err != nil {
		t.Fatal(err)
	}

	if repo.filter.State != StateRunning || repo.filter.Prefix != "WEL" || repo.filter.Limit != 3 || repo.filter.Offset != 0 ||
		!repo.filter.Now.Equal(now) {
		t.Fatalf("filter %+v", repo.filter)
	}

	if len(page.Items) != 2 || page.NextOffset != 2 {
		t.Fatalf("page %+v", page)
	}

	repo.listed = repo.listed[:1]

	page, err = service.ListCoupons(context.Background(), "", "", 0, "2")
	if err != nil || repo.filter.Offset != 2 || repo.filter.Limit != defaultPageSize+1 || page.NextOffset != 0 {
		t.Fatalf("err %v, filter %+v, page %+v", err, repo.filter, page)
	}

	for _, bad := range []struct{ state, token string }{{"old", ""}, {"", "x"}, {"", "-1"}, {"", "02"}} {
		var invalidErr *InvalidError
		if _, err := service.ListCoupons(context.Background(), bad.state, "", 0, bad.token); !errors.As(err, &invalidErr) {
			t.Fatalf("%v: got %v", bad, err)
		}
	}
}

func TestUpdatingACoupon(t *testing.T) {
	service, repo := newTestService()
	repo.coupons["SAVE"] = pricing.Coupon{ID: "coupon-1", Code: "SAVE", ValidFrom: now, ValidUntil: now.Add(time.Hour)}

	change := CouponChange{Description: ptr("  autumn  "), Active: ptr(false), MaxRedemptions: ptr(0)}
	if _, err := service.UpdateCoupon(context.Background(), "save", change, staffID); err != nil {
		t.Fatal(err)
	}

	if got := repo.changes[0]; *got.Description != "autumn" || *got.Active || *got.MaxRedemptions != 0 {
		t.Fatalf("change %+v", got)
	}

	for _, bad := range []CouponChange{
		{ValidUntil: ptr(now)},
		{MaxRedemptions: ptr(-1)},
		{PerRiderLimit: ptr(0)},
		{MinimumFareAmount: ptr(dec("-5"))},
	} {
		var invalidErr *InvalidError
		if _, err := service.UpdateCoupon(context.Background(), "SAVE", bad, staffID); !errors.As(err, &invalidErr) {
			t.Fatalf("%+v: got %v", bad, err)
		}
	}

	if _, err := service.UpdateCoupon(context.Background(), "NOPE", change, staffID); !errors.Is(err, pricing.ErrCouponNotFound) {
		t.Fatalf("got %v", err)
	}

	if _, err := service.UpdateCoupon(context.Background(), "a b", change, staffID); !errors.Is(err, pricing.ErrCouponNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestListingACouponsUse(t *testing.T) {
	service, repo := newTestService()
	repo.coupons["SAVE"] = pricing.Coupon{ID: "coupon-1", Code: "SAVE"}

	if _, err := service.ListRedemptions(context.Background(), "save", 10, "20"); err != nil {
		t.Fatal(err)
	}

	if repo.offsets != [2]int{20, 11} {
		t.Fatalf("offsets %v", repo.offsets)
	}

	if _, err := service.ListRedemptions(context.Background(), "OTHER", 10, ""); !errors.Is(err, pricing.ErrCouponNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestTheAutomaticDiscountsAreChecked(t *testing.T) {
	service, repo := newTestService()

	valid := SettingsInput{FirstRidePercent: dec("30"), FirstRideMaxAmount: ptr(dec("3000")), LoyaltyEvery: 5, LoyaltyPercent: dec("15")}

	saved, err := service.UpdateSettings(context.Background(), valid, staffID)
	if err != nil || saved.UpdatedBy != staffID || repo.settings.LoyaltyEvery != 5 {
		t.Fatalf("saved %+v, %v", saved, err)
	}

	off := SettingsInput{FirstRidePercent: dec("0"), LoyaltyEvery: 0, LoyaltyPercent: dec("0")}
	if _, err := service.UpdateSettings(context.Background(), off, staffID); err != nil {
		t.Fatalf("turning both off: %v", err)
	}

	for _, bad := range []SettingsInput{
		{FirstRidePercent: dec("101"), LoyaltyPercent: dec("10")},
		{FirstRidePercent: dec("-1"), LoyaltyPercent: dec("10")},
		{FirstRidePercent: dec("10"), LoyaltyPercent: dec("10"), LoyaltyEvery: 1},
		{FirstRidePercent: dec("10"), LoyaltyPercent: dec("10"), LoyaltyEvery: 101},
		{FirstRidePercent: dec("10"), LoyaltyPercent: dec("10"), LoyaltyMaxAmount: ptr(dec("0"))},
	} {
		var invalidErr *InvalidError
		if _, err := service.UpdateSettings(context.Background(), bad, staffID); !errors.As(err, &invalidErr) {
			t.Fatalf("%+v: got %v", bad, err)
		}
	}
}
