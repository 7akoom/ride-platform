package grpc

import (
	"context"
	"errors"
	"testing"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeStaff struct {
	allowed   bool
	err       error
	asked     []string
	targets   []string
	completed map[string]codes.Code
}

func (f *fakeStaff) Authorize(_ context.Context, identityID, permission, method, targetID string) (bool, string, error) {
	f.asked = append(f.asked, identityID+" "+permission+" "+method)
	f.targets = append(f.targets, targetID)

	if f.err != nil {
		return false, "", f.err
	}

	return f.allowed, "audit-1", nil
}

func (f *fakeStaff) Complete(_ context.Context, auditEntryID string, code codes.Code) {
	if f.completed == nil {
		f.completed = map[string]codes.Code{}
	}

	f.completed[auditEntryID] = code
}

func callAsStaff(t *testing.T, staff StaffAuthorizer, method string, request any, result error) (bool, error) {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(newOwnershipResolver(), staff)
	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: "identity-1", SessionID: "s"})

	ran := false

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: pricingRPCPrefix + method},
		func(context.Context, any) (any, error) {
			ran = true

			return nil, result
		})

	return ran, err
}

func TestStaffWithPricingManageSetPrices(t *testing.T) {
	staff := &fakeStaff{allowed: true}

	ran, err := callAsStaff(t, staff, "CreateZoneSurge", &pricingv1.CreateZoneSurgeRequest{ZoneId: "zone-1"}, nil)
	if !ran || err != nil {
		t.Fatalf("ran=%v err=%v", ran, err)
	}

	if staff.asked[0] != "identity-1 pricing.manage /ride.pricing.v1.PricingService/CreateZoneSurge" || staff.targets[0] != "zone-1" {
		t.Fatalf("asked %v for %v", staff.asked, staff.targets)
	}

	if staff.completed["audit-1"] != codes.OK {
		t.Fatalf("completed %v", staff.completed)
	}
}

func TestTheAuditTargetIsWhatTheRequestChanges(t *testing.T) {
	cases := map[string]struct {
		method  string
		request any
		want    string
	}{
		"a rule":        {"UpdateSurgeRule", &pricingv1.UpdateSurgeRuleRequest{RuleId: "rule-1"}, "rule-1"},
		"a zone surge":  {"EndZoneSurge", &pricingv1.EndZoneSurgeRequest{ZoneSurgeId: "surge-1"}, "surge-1"},
		"a city's card": {"SetRateCard", &pricingv1.SetRateCardRequest{CityId: "city-1"}, "city-1"},
		"everywhere":    {"SetRateCard", &pricingv1.SetRateCardRequest{}, ""},
	}

	for name, tc := range cases {
		staff := &fakeStaff{allowed: true}

		if _, err := callAsStaff(t, staff, tc.method, tc.request, nil); err != nil {
			t.Fatal(err)
		}

		if staff.targets[0] != tc.want {
			t.Fatalf("%s: target %q, want %q", name, staff.targets[0], tc.want)
		}
	}
}

func TestPricingAdminIsClosedWithoutThePermission(t *testing.T) {
	for method := range staffPermissions {
		name := method[len(pricingRPCPrefix):]

		if ran, err := callAsStaff(t, &fakeStaff{}, name, &pricingv1.ListRateCardsRequest{}, nil); ran || status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s: ran=%v err=%v", name, ran, err)
		}

		if ran, err := callAsStaff(t, &fakeStaff{err: errors.New("staff-service down")}, name, &pricingv1.ListRateCardsRequest{}, nil); ran || status.Code(err) != codes.Unavailable {
			t.Fatalf("%s with staff-service down: ran=%v err=%v", name, ran, err)
		}
	}

	if ran, err := callAsStaff(t, nil, "SetRateCard", &pricingv1.SetRateCardRequest{}, nil); ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("no authorizer: ran=%v err=%v", ran, err)
	}
}

func TestAFailedStaffActionIsRecordedAsFailed(t *testing.T) {
	staff := &fakeStaff{allowed: true}

	if _, err := callAsStaff(t, staff, "RetireRateCard", &pricingv1.RetireRateCardRequest{}, status.Error(codes.FailedPrecondition, "base")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err %v", err)
	}

	if staff.completed["audit-1"] != codes.FailedPrecondition {
		t.Fatalf("completed %v", staff.completed)
	}
}

func TestEveryStaffMethodNamesItsPermission(t *testing.T) {
	for method, level := range methodAccess {
		_, named := staffPermissions[method]

		if level == accessStaff && !named {
			t.Errorf("%s is accessStaff without a permission", method)
		}

		if level != accessStaff && named {
			t.Errorf("%s names a permission but is not accessStaff", method)
		}
	}
}

func TestRidersQuoteOnlyForThemselvesAndNeverClaim(t *testing.T) {
	resolver := newOwnershipResolver()
	quote := func(rider string) *pricingv1.QuoteTripRequest { return &pricingv1.QuoteTripRequest{RiderId: rider} }

	if code := callOwnershipAs(t, "id-rider-a", resolver, "QuoteTrip", quote("rider-a")); code != codes.OK {
		t.Fatalf("own quote: %v", code)
	}

	if code := callOwnershipAs(t, "id-rider-b", resolver, "QuoteTrip", quote("rider-a")); code != codes.PermissionDenied {
		t.Fatalf("someone else's: %v", code)
	}

	for _, method := range []string{"ClaimQuote", "ReleaseQuote", "CalculateFare"} {
		if code := callOwnershipAs(t, "id-rider-a", resolver, method, &pricingv1.ClaimQuoteRequest{RiderId: "rider-a"}); code != codes.PermissionDenied {
			t.Fatalf("%s: %v", method, code)
		}
	}
}
