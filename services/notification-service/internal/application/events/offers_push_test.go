package events_test

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/events"
)

type fakeAudience struct {
	riders []string
	asked  []string
}

func (f *fakeAudience) RidersWithDevices(_ context.Context, after string, limit int) ([]string, error) {
	f.asked = append(f.asked, after)

	start := 0
	for i, id := range f.riders {
		if id > after {
			start = i

			break
		}

		start = len(f.riders)
	}

	end := start + limit
	if end > len(f.riders) {
		end = len(f.riders)
	}

	return f.riders[start:end], nil
}

func TestTheRiderHearsTheirNextRideHasTheLoyaltyDiscount(t *testing.T) {
	h := newHarness()

	err := h.handler().Dispatch(context.Background(), events.SubjectLoyaltyDue,
		envelopeWithPayload("evt-20", `{"rider_id":"rider-1","percent":"20","trip_number":10}`))
	if err != nil {
		t.Fatal(err)
	}

	call := h.notifications.sendCalls[0]
	if call.RecipientID != "rider-1" || call.EventKey != "promo.loyalty_due" || call.Variables["percent"] != "20" || call.IdempotencyKey != "evt-20" {
		t.Fatalf("call %+v", call)
	}
}

func TestAFeaturedCouponReachesEveryRiderWithAPhoneOnce(t *testing.T) {
	h := newHarness()

	var riders []string
	for i := 0; i < 450; i++ {
		riders = append(riders, string(rune('a'+i/26/26%26))+string(rune('a'+i/26%26))+string(rune('a'+i%26)))
	}

	audience := &fakeAudience{riders: riders}
	handler := events.NewHandler(h.notifications, h.trips, h.drivers, testLogger(), events.WithRiderAudience(audience))

	err := handler.Dispatch(context.Background(), events.SubjectFeaturedOffer,
		envelopeWithPayload("evt-21", `{"coupon_code":"AUTUMN","description":"20% off this week"}`))
	if err != nil {
		t.Fatal(err)
	}

	if len(h.notifications.sendCalls) != 450 || len(audience.asked) != 3 {
		t.Fatalf("%d sent over %d pages", len(h.notifications.sendCalls), len(audience.asked))
	}

	call := h.notifications.sendCalls[449]
	if call.EventKey != "promo.featured_coupon" || call.Variables["code"] != "AUTUMN" || call.Data["coupon_code"] != "AUTUMN" ||
		call.IdempotencyKey != "evt-21:"+riders[449] || call.RecipientID != riders[449] {
		t.Fatalf("call %+v", call)
	}
}

func TestWithoutAnAudienceAFeaturedCouponIsSkipped(t *testing.T) {
	h := newHarness()

	if err := h.handler().Dispatch(context.Background(), events.SubjectFeaturedOffer,
		envelopeWithPayload("evt-22", `{"coupon_code":"AUTUMN"}`)); err != nil || len(h.notifications.sendCalls) != 0 {
		t.Fatalf("%v, %d sent", err, len(h.notifications.sendCalls))
	}
}
