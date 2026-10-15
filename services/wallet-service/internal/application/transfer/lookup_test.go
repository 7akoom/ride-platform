package transfer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type fakePreflight struct {
	sentAmount wallet.Money
	sentCount  int
	lookups    []time.Time
	forgot     time.Time
}

func (p *fakePreflight) SentLastDay(context.Context, string) (wallet.Money, int, error) {
	return p.sentAmount, p.sentCount, nil
}

func (p *fakePreflight) Lookups(_ context.Context, _ string, since time.Time) (int, error) {
	count := 0

	for _, at := range p.lookups {
		if at.After(since) {
			count++
		}
	}

	return count, nil
}

func (p *fakePreflight) RecordLookup(_ context.Context, _ string, at, forgetBefore time.Time) error {
	p.lookups = append(p.lookups, at)
	p.forgot = forgetBefore

	return nil
}

type fakeDirectory map[string][2]string // identity -> rider, name

func (d fakeDirectory) RiderByIdentity(_ context.Context, identityID string) (string, string, error) {
	entry := d[identityID]
	return entry[0], entry[1], nil
}

var lookupNow = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

func newLookupService(limits LookupLimits) (*Service, *fakePreflight, *fakeIdentity) {
	service, _, identity := newTestService()
	preflight := &fakePreflight{sentAmount: dec(7500), sentCount: 2}
	directory := fakeDirectory{
		recipientIdentity: {recipientRider, "  Haidar  Kamal Ali "},
		senderIdentity:    {senderRider, "Me"},
	}
	service.WithLookups(preflight, directory, limits)
	service.now = func() time.Time { return lookupNow }

	return service, preflight, identity
}

func lookup(phone string) LookupInput {
	return LookupInput{RiderID: senderRider, IdentityID: senderIdentity, Phone: phone}
}

func TestTransferOptions(t *testing.T) {
	service, _, _ := newLookupService(LookupLimits{PerHour: 3, PerDay: 5})

	options, err := service.Options(context.Background(), senderRider)
	if err != nil {
		t.Fatal(err)
	}

	if options.CurrencyCode != "IQD" || !options.MinAmount.Equal(dec(250)) || !options.MaxAmount.Equal(dec(100000)) ||
		!options.DailyAmount.Equal(dec(200000)) || options.DailyCount != 5 ||
		!options.SentAmount.Equal(dec(7500)) || options.SentCount != 2 {
		t.Fatalf("options %+v", options)
	}

	if _, err := service.Options(context.Background(), " "); !errors.Is(err, ErrSenderRequired) {
		t.Fatalf("no rider: %v", err)
	}
}

func TestLookingUpARecipient(t *testing.T) {
	service, preflight, _ := newLookupService(LookupLimits{PerHour: 3, PerDay: 5})

	found, err := service.Lookup(context.Background(), lookup(recipientPhone))
	if err != nil {
		t.Fatal(err)
	}

	if found.Result != RecipientFound || found.DisplayName != "Haidar A." {
		t.Fatalf("found %+v", found)
	}

	if len(preflight.lookups) != 1 || !preflight.forgot.Equal(lookupNow.Add(-24*time.Hour)) {
		t.Fatalf("lookups %v, forgot %v", preflight.lookups, preflight.forgot)
	}

	missing, err := service.Lookup(context.Background(), lookup("+9647700000000"))
	if err != nil || missing.Result != RecipientNotFound || missing.DisplayName != "" {
		t.Fatalf("missing %+v, %v", missing, err)
	}

	self, err := service.Lookup(context.Background(), lookup(senderPhone))
	if err != nil || self.Result != RecipientSelf || self.DisplayName != "" {
		t.Fatalf("self %+v, %v", self, err)
	}
}

func TestLookupFindsOnlyWhomASendWould(t *testing.T) {
	service, _, identity := newLookupService(LookupLimits{PerHour: 3, PerDay: 5})
	// Registered, but no rider profile (a driver, say).
	identity.phones["+9647711111111"] = "identity-driver"

	result, err := service.Lookup(context.Background(), lookup("+9647711111111"))
	if err != nil || result.Result != RecipientNotFound {
		t.Fatalf("no rider profile: %+v, %v", result, err)
	}
}

func TestLookupRefusesBadInput(t *testing.T) {
	service, preflight, _ := newLookupService(LookupLimits{PerHour: 3, PerDay: 5})

	cases := map[string]struct {
		input LookupInput
		want  error
	}{
		"no rider":    {LookupInput{IdentityID: senderIdentity, Phone: recipientPhone}, ErrSenderRequired},
		"no person":   {LookupInput{RiderID: senderRider, Phone: recipientPhone}, ErrIdentityRequired},
		"local phone": {lookup("07701234567"), ErrInvalidPhone},
	}

	for name, c := range cases {
		if _, err := service.Lookup(context.Background(), c.input); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	if len(preflight.lookups) != 0 {
		t.Fatalf("bad input was counted: %v", preflight.lookups)
	}
}

func TestLookupLimits(t *testing.T) {
	service, preflight, _ := newLookupService(LookupLimits{PerHour: 2, PerDay: 3})

	for i := 0; i < 2; i++ {
		if _, err := service.Lookup(context.Background(), lookup(recipientPhone)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := service.Lookup(context.Background(), lookup(recipientPhone)); !errors.Is(err, ErrLookupLimit) {
		t.Fatalf("third in the hour: %v", err)
	}

	// An hour later the hourly limit is clear; the daily one still counts.
	service.now = func() time.Time { return lookupNow.Add(61 * time.Minute) }

	if _, err := service.Lookup(context.Background(), lookup(recipientPhone)); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Lookup(context.Background(), lookup(recipientPhone)); !errors.Is(err, ErrLookupLimit) {
		t.Fatalf("fourth in the day: %v", err)
	}

	if len(preflight.lookups) != 3 {
		t.Fatalf("refused lookups were counted: %d", len(preflight.lookups))
	}
}

func TestLookupsNeedSetting(t *testing.T) {
	service, _, _ := newTestService()

	if _, err := service.Lookup(context.Background(), lookup(recipientPhone)); !errors.Is(err, ErrLookupsUnavailable) {
		t.Fatalf("lookup: %v", err)
	}

	if _, err := service.Options(context.Background(), senderRider); !errors.Is(err, ErrLookupsUnavailable) {
		t.Fatalf("options: %v", err)
	}
}

func TestShortName(t *testing.T) {
	cases := map[string]string{
		"Haidar Kamal":    "Haidar K.",
		"حيدر كمال":       "حيدر ك.",
		"  Sara  ":        "Sara",
		"":                "",
		"Omar Azad Hasan": "Omar H.",
	}

	for name, want := range cases {
		if got := ShortName(name); got != want {
			t.Fatalf("ShortName(%q) = %q, want %q", name, got, want)
		}
	}
}
