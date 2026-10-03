package incentives

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	maxDuration       = 31 * 24 * time.Hour
	startGrace        = 5 * time.Minute
	maxTiers          = 5
	maxTierTrips      = 1000
	maxNameLength     = 80
	maxTextLength     = 500
	maxKeyLength      = 120
	defaultPageSize   = 20
	maxPageSize       = 100
	visibleAfterEnded = 14 * 24 * time.Hour
)

var (
	uuidShape   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	clockShape  = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
	maxAmount   = decimal.NewFromInt(100_000_000)
	ratingFloor = decimal.NewFromInt(1)
	ratingCeil  = decimal.NewFromInt(5)
)

// Settings are the deployment's.
type Settings struct {
	// TimeZone is used for daily hours when a campaign names none.
	TimeZone string
	// SettleDelay is how long after a campaign ends it is paid, so trips
	// finishing at the end are counted.
	SettleDelay time.Duration
}

type Service struct {
	store    Store
	trips    Trips
	drivers  Drivers
	settings Settings
	logger   *slog.Logger
	now      func() time.Time
}

func NewService(store Store, trips Trips, drivers Drivers, settings Settings, logger *slog.Logger) *Service {
	if store == nil || trips == nil || drivers == nil || logger == nil {
		panic("incentive service dependencies are required")
	}

	if _, err := time.LoadLocation(settings.TimeZone); err != nil {
		panic("incentive time zone is not valid: " + err.Error())
	}

	return &Service{store: store, trips: trips, drivers: drivers, settings: settings, logger: logger,
		now: func() time.Time { return time.Now().UTC() }}
}

// CreateInput is a new campaign as staff give it.
type CreateInput struct {
	Name                string
	Description         string
	StartsAt            time.Time
	EndsAt              time.Time
	CityID              string
	ZoneIDs             []string
	VehicleClass        string
	DailyStart          string
	DailyEnd            string
	TimeZone            string
	MinAcceptanceRate   string
	MaxCancellationRate string
	MinRating           string
	Tiers               []Tier
	IdempotencyKey      string
	CreatedBy           string
}

// Create checks and saves a campaign. A retry with the same key returns the
// campaign made the first time.
func (s *Service) Create(ctx context.Context, in CreateInput) (Campaign, error) {
	key := strings.TrimSpace(in.IdempotencyKey)
	if len(key) > maxKeyLength {
		return Campaign{}, ErrIdempotencyKey
	}

	c, err := s.check(in)
	if err != nil {
		return Campaign{}, err
	}

	if key != "" {
		earlier, found, err := s.store.FindByKey(ctx, key)
		if err != nil {
			return Campaign{}, fmt.Errorf("look up the idempotency key: %w", err)
		}

		if found {
			if earlier.Name != c.Name || !earlier.StartsAt.Equal(c.StartsAt) || !earlier.EndsAt.Equal(c.EndsAt) {
				return Campaign{}, ErrKeyReused
			}

			return earlier, nil
		}
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Campaign{}, fmt.Errorf("read wallet config: %w", err)
	}

	c.CurrencyCode = config.CurrencyCode

	created, err := s.store.Create(ctx, c, uuidOrEmpty(in.CreatedBy), key)
	if err != nil {
		return Campaign{}, fmt.Errorf("create campaign: %w", err)
	}

	return created, nil
}

func (s *Service) check(in CreateInput) (Campaign, error) {
	now := s.now()

	c := Campaign{
		Name:        strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description),
		StartsAt:    in.StartsAt.UTC(),
		EndsAt:      in.EndsAt.UTC(),
		Status:      StatusActive,
	}

	if c.Name == "" || utf8.RuneCountInString(c.Name) > maxNameLength || utf8.RuneCountInString(c.Description) > maxTextLength {
		return Campaign{}, ErrInvalidName
	}

	switch {
	case c.StartsAt.IsZero(), c.EndsAt.IsZero(),
		!c.EndsAt.After(c.StartsAt), !c.EndsAt.After(now),
		c.EndsAt.Sub(c.StartsAt) > maxDuration,
		c.StartsAt.Before(now.Add(-startGrace)):
		return Campaign{}, ErrInvalidPeriod
	}

	c.CityID = strings.ToLower(strings.TrimSpace(in.CityID))
	if c.CityID != "" && !uuidShape.MatchString(c.CityID) {
		return Campaign{}, ErrInvalidScope
	}

	for _, zone := range in.ZoneIDs {
		zone = strings.ToLower(strings.TrimSpace(zone))
		if !uuidShape.MatchString(zone) {
			return Campaign{}, ErrInvalidScope
		}

		if !slices.Contains(c.ZoneIDs, zone) {
			c.ZoneIDs = append(c.ZoneIDs, zone)
		}
	}

	switch c.VehicleClass = strings.ToLower(strings.TrimSpace(in.VehicleClass)); c.VehicleClass {
	case "", "economy", "comfort":
	default:
		return Campaign{}, ErrInvalidScope
	}

	start, end := strings.TrimSpace(in.DailyStart), strings.TrimSpace(in.DailyEnd)

	if start != "" || end != "" {
		var ok bool

		if c.DailyStart, ok = minuteOf(start); !ok {
			return Campaign{}, ErrInvalidHours
		}

		if c.DailyEnd, ok = minuteOf(end); !ok {
			return Campaign{}, ErrInvalidHours
		}
	}

	c.TimeZone = strings.TrimSpace(in.TimeZone)
	if c.TimeZone == "" {
		c.TimeZone = s.settings.TimeZone
	}

	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return Campaign{}, ErrInvalidTimeZone
	}

	var err error

	if c.MinAcceptanceRate, err = percentage(in.MinAcceptanceRate); err != nil {
		return Campaign{}, err
	}

	if c.MaxCancellationRate, err = percentage(in.MaxCancellationRate); err != nil {
		return Campaign{}, err
	}

	if rating := strings.TrimSpace(in.MinRating); rating != "" {
		value, err := decimal.NewFromString(rating)
		if err != nil || value.LessThan(ratingFloor) || value.GreaterThan(ratingCeil) || !value.Equal(value.Round(2)) {
			return Campaign{}, ErrInvalidConditions
		}

		c.MinRating = &value
	}

	if len(in.Tiers) == 0 || len(in.Tiers) > maxTiers {
		return Campaign{}, ErrInvalidTiers
	}

	for i, tier := range in.Tiers {
		switch {
		case tier.Trips < 1, tier.Trips > maxTierTrips,
			!tier.Amount.IsPositive(), tier.Amount.GreaterThan(maxAmount), !tier.Amount.Equal(tier.Amount.Round(3)):
			return Campaign{}, ErrInvalidTiers
		case i > 0 && (tier.Trips <= in.Tiers[i-1].Trips || !tier.Amount.GreaterThan(in.Tiers[i-1].Amount)):
			return Campaign{}, ErrInvalidTiers
		}
	}

	c.Tiers = slices.Clone(in.Tiers)

	return c, nil
}

func minuteOf(clock string) (int, bool) {
	m := clockShape.FindStringSubmatch(clock)
	if m == nil {
		return 0, false
	}

	return int(m[1][0]-'0')*600 + int(m[1][1]-'0')*60 + int(m[2][0]-'0')*10 + int(m[2][1]-'0'), true
}

// ClockOf formats minutes after midnight as HH:MM.
func ClockOf(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

func percentage(raw string) (*wallet.Money, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() || value.GreaterThan(hundred) || !value.Equal(value.Round(2)) {
		return nil, ErrInvalidConditions
	}

	return &value, nil
}

func (s *Service) Get(ctx context.Context, id string) (Campaign, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !uuidShape.MatchString(id) {
		return Campaign{}, ErrCampaignNotFound
	}

	return s.store.Get(ctx, id)
}

// Now is the service's clock, for showing states.
func (s *Service) Now() time.Time {
	return s.now()
}

type CampaignPage struct {
	Campaigns     []Campaign
	NextPageToken string
}

// List returns campaigns newest first; a state narrows them.
func (s *Service) List(ctx context.Context, state State, pageSize int, pageToken string) (CampaignPage, error) {
	switch state {
	case "", StateScheduled, StateRunning, StateSettling, StateSettled, StateCancelled:
	default:
		return CampaignPage{}, ErrInvalidStatus
	}

	size, err := pageSizeOf(pageSize)
	if err != nil {
		return CampaignPage{}, err
	}

	after := strings.ToLower(strings.TrimSpace(pageToken))
	if after != "" && !uuidShape.MatchString(after) {
		return CampaignPage{}, ErrInvalidPage
	}

	found, err := s.store.List(ctx, CampaignsQuery{Status: string(state), AfterID: after, Limit: size + 1, Now: s.now()})
	if err != nil {
		return CampaignPage{}, fmt.Errorf("list campaigns: %w", err)
	}

	page := CampaignPage{Campaigns: found}

	if len(found) > size {
		page.Campaigns = found[:size]
		page.NextPageToken = found[size-1].ID
	}

	return page, nil
}

// Cancel stops a campaign that has not ended; nobody is paid.
func (s *Service) Cancel(ctx context.Context, id, reason string) (Campaign, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxTextLength {
		return Campaign{}, ErrReasonRequired
	}

	current, err := s.Get(ctx, id)
	if err != nil {
		return Campaign{}, err
	}

	cancelled, err := s.store.Cancel(ctx, current.ID, reason, s.now())
	if err != nil {
		return Campaign{}, fmt.Errorf("cancel campaign: %w", err)
	}

	return cancelled, nil
}

type PayoutPage struct {
	Payouts       []Payout
	NextPageToken string
}

func (s *Service) Payouts(ctx context.Context, campaignID string, pageSize int, pageToken string) (PayoutPage, error) {
	campaign, err := s.Get(ctx, campaignID)
	if err != nil {
		return PayoutPage{}, err
	}

	size, err := pageSizeOf(pageSize)
	if err != nil {
		return PayoutPage{}, err
	}

	after := strings.ToLower(strings.TrimSpace(pageToken))
	if after != "" && !uuidShape.MatchString(after) {
		return PayoutPage{}, ErrInvalidPage
	}

	found, err := s.store.Payouts(ctx, campaign.ID, after, size+1)
	if err != nil {
		return PayoutPage{}, fmt.Errorf("list payouts: %w", err)
	}

	page := PayoutPage{Payouts: found}

	if len(found) > size {
		page.Payouts = found[:size]
		page.NextPageToken = found[size-1].DriverID
	}

	return page, nil
}

// DriverView is a campaign as one driver sees it.
type DriverView struct {
	Campaign         Campaign
	CompletedTrips   int
	Reached          *Tier
	Next             *Tier
	AcceptanceRate   wallet.Money
	CancellationRate wallet.Money
	Unmet            []string
	Payout           *Payout
}

// ForDriver shows the driver the campaigns for their class that are coming
// up, running, or ended in the last two weeks, with their progress.
func (s *Service) ForDriver(ctx context.Context, driverID string) ([]DriverView, error) {
	driverID = strings.ToLower(strings.TrimSpace(driverID))
	if !uuidShape.MatchString(driverID) {
		return nil, ErrDriverNotFound
	}

	profile, err := s.drivers.Profile(ctx, driverID)
	if err != nil {
		return nil, err
	}

	now := s.now()

	campaigns, err := s.store.Visible(ctx, now.Add(-visibleAfterEnded))
	if err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}

	campaigns = slices.DeleteFunc(campaigns, func(c Campaign) bool {
		return c.VehicleClass != "" && c.VehicleClass != profile.VehicleClass
	})

	ids := make([]string, 0, len(campaigns))
	for _, c := range campaigns {
		ids = append(ids, c.ID)
	}

	payouts, err := s.store.DriverPayouts(ctx, driverID, ids)
	if err != nil {
		return nil, fmt.Errorf("read the driver's payouts: %w", err)
	}

	views := make([]DriverView, 0, len(campaigns))

	for _, c := range campaigns {
		view := DriverView{Campaign: c, AcceptanceRate: hundred, CancellationRate: decimal.Zero}
		view.Reached, view.Next = c.Reached(0)

		if payout, done := payouts[c.ID]; done {
			view.Payout = &payout
		}

		if c.StateAt(now) != StateScheduled {
			activity, err := s.trips.Driver(ctx, driverID, c)
			if err != nil {
				return nil, fmt.Errorf("count the driver's trips: %w", err)
			}

			view.CompletedTrips = activity.CompletedTrips
			view.Reached, view.Next = c.Reached(activity.CompletedTrips)
			view.AcceptanceRate, view.CancellationRate = Rates(activity)
			view.Unmet = c.Unmet(view.AcceptanceRate, view.CancellationRate, profile.Rating)
		}

		views = append(views, view)
	}

	return views, nil
}

// SettleDue pays one campaign that has ended, if any is due; done is false
// when none was.
func (s *Service) SettleDue(ctx context.Context, lease time.Duration) (done bool, err error) {
	now := s.now()

	c, found, err := s.store.ClaimDue(ctx, now.Add(-s.settings.SettleDelay), lease)
	if err != nil || !found {
		return false, err
	}

	paid, notEligible := 0, 0
	token := ""

	for {
		page, next, err := s.trips.Drivers(ctx, c, c.LowestTier(), token)
		if err != nil {
			return true, fmt.Errorf("count drivers' trips for campaign %s: %w", c.ID, err)
		}

		for _, a := range page {
			var rating *wallet.Money

			if c.MinRating != nil {
				profile, err := s.drivers.Profile(ctx, a.DriverID)

				switch {
				case errors.Is(err, ErrDriverNotFound):
					s.logger.Warn("incentive driver not found; settled without a rating", "campaign_id", c.ID, "driver_id", a.DriverID)
				case err != nil:
					return true, fmt.Errorf("read driver %s: %w", a.DriverID, err)
				default:
					rating = profile.Rating
				}
			}

			payout, err := s.store.Settle(ctx, c, c.Decide(a, rating))
			if err != nil {
				return true, fmt.Errorf("settle driver %s of campaign %s: %w", a.DriverID, c.ID, err)
			}

			if payout.Status == PayoutPaid {
				paid++
			} else {
				notEligible++
			}
		}

		if next == "" {
			break
		}

		token = next
	}

	if err := s.store.MarkSettled(ctx, c.ID, s.now()); err != nil {
		return true, fmt.Errorf("mark campaign %s settled: %w", c.ID, err)
	}

	s.logger.Info("incentive campaign settled", "campaign_id", c.ID, "paid", paid, "not_eligible", notEligible)

	return true, nil
}

func pageSizeOf(size int) (int, error) {
	switch {
	case size < 0:
		return 0, ErrInvalidPage
	case size == 0:
		return defaultPageSize, nil
	case size > maxPageSize:
		return maxPageSize, nil
	}

	return size, nil
}

func uuidOrEmpty(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if !uuidShape.MatchString(id) {
		return ""
	}

	return id
}
