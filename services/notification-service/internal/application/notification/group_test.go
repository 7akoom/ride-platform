package notification

import (
	"slices"
	"testing"
)

func TestGroupFamilies(t *testing.T) {
	cases := map[Group]struct {
		families []string
		exclude  bool
	}{
		GroupAll:     {nil, false},
		GroupTrips:   {[]string{"trip"}, false},
		GroupWallet:  {[]string{"wallet"}, false},
		GroupOffers:  {[]string{"promo", "offer"}, false},
		GroupSupport: {[]string{"support"}, false},
		GroupOther:   {[]string{"trip", "wallet", "promo", "offer", "support"}, true},
	}

	for group, want := range cases {
		families, exclude := group.Families()
		if !slices.Equal(families, want.families) || exclude != want.exclude {
			t.Errorf("%q: %v, %v; want %v, %v", group, families, exclude, want.families, want.exclude)
		}
	}
}

func TestGroupOf(t *testing.T) {
	cases := map[string]Group{
		"trip.started":              GroupTrips,
		"wallet.topped_up":          GroupWallet,
		"offer.new":                 GroupOffers,
		"promo.coupon":              GroupOffers,
		"support.ticket_replied":    GroupSupport,
		"account.data_export_ready": GroupOther,
		"tripx.started":             GroupOther,
		"":                          GroupOther,
	}

	for key, want := range cases {
		if got := GroupOf(key); got != want {
			t.Errorf("GroupOf(%q) = %q, want %q", key, got, want)
		}
	}
}
