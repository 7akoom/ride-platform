// Package activity counts what drivers did in a period: trips completed,
// offers accepted and declined, trips they cancelled. wallet-service pays
// incentive campaigns from it.
package activity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// MaxPeriod is the longest period one question may cover.
	MaxPeriod       = 93 * 24 * time.Hour
	minutesInDay    = 24 * 60
	defaultPageSize = 200
	maxPageSize     = 1000
)

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var (
	ErrInvalidPeriod    = errors.New("the period must have from before to, at most 93 days apart")
	ErrInvalidScope     = errors.New("city_id, zone_ids and vehicle_class must be ids and economy or comfort")
	ErrInvalidHours     = errors.New("daily hours must be minutes 0-1440")
	ErrInvalidTimeZone  = errors.New("time_zone is not an IANA time zone")
	ErrDriverIDRequired = errors.New("driver_id is required")
	ErrInvalidPage      = errors.New("page_size or page_token is not valid")
)

// Scope is the period and the trips that count.
type Scope struct {
	From         time.Time
	To           time.Time
	CityID       string
	ZoneIDs      []string
	VehicleClass string
	// DailyStart and DailyEnd are minutes after local midnight in TimeZone;
	// equal means all day. The window may cross midnight.
	DailyStart int
	DailyEnd   int
	TimeZone   string
}

// AllDay reports that no daily hours apply.
func (s Scope) AllDay() bool {
	return s.DailyStart == s.DailyEnd
}

// Activity is one driver's counts.
type Activity struct {
	DriverID            string
	CompletedTrips      int
	OffersAccepted      int
	OffersDeclined      int
	DriverCancellations int
}

// Store counts from the trips and offers.
type Store interface {
	Driver(ctx context.Context, driverID string, scope Scope) (Activity, error)
	// Drivers returns, by driver id after afterID, those with at least
	// minCompleted completed trips in scope.
	Drivers(ctx context.Context, scope Scope, minCompleted int, afterID string, limit int) ([]Activity, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	if store == nil {
		panic("activity store is required")
	}

	return &Service{store: store}
}

func (s *Service) Driver(ctx context.Context, driverID string, scope Scope) (Activity, error) {
	driverID = strings.ToLower(strings.TrimSpace(driverID))
	if !uuidShape.MatchString(driverID) {
		return Activity{}, ErrDriverIDRequired
	}

	clean, err := checkScope(scope)
	if err != nil {
		return Activity{}, err
	}

	found, err := s.store.Driver(ctx, driverID, clean)
	if err != nil {
		return Activity{}, fmt.Errorf("count driver activity: %w", err)
	}

	return found, nil
}

// Page is one page of drivers' activity.
type Page struct {
	Drivers       []Activity
	NextPageToken string
}

func (s *Service) Drivers(ctx context.Context, scope Scope, minCompleted, pageSize int, pageToken string) (Page, error) {
	clean, err := checkScope(scope)
	if err != nil {
		return Page{}, err
	}

	switch {
	case pageSize < 0, minCompleted < 0:
		return Page{}, ErrInvalidPage
	case pageSize == 0:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		pageSize = maxPageSize
	}

	after := strings.ToLower(strings.TrimSpace(pageToken))
	if after != "" && !uuidShape.MatchString(after) {
		return Page{}, ErrInvalidPage
	}

	found, err := s.store.Drivers(ctx, clean, minCompleted, after, pageSize+1)
	if err != nil {
		return Page{}, fmt.Errorf("count drivers' activity: %w", err)
	}

	page := Page{Drivers: found}

	if len(found) > pageSize {
		page.Drivers = found[:pageSize]
		page.NextPageToken = found[pageSize-1].DriverID
	}

	return page, nil
}

func checkScope(s Scope) (Scope, error) {
	if s.From.IsZero() || s.To.IsZero() || !s.From.Before(s.To) || s.To.Sub(s.From) > MaxPeriod {
		return Scope{}, ErrInvalidPeriod
	}

	s.CityID = strings.ToLower(strings.TrimSpace(s.CityID))
	if s.CityID != "" && !uuidShape.MatchString(s.CityID) {
		return Scope{}, ErrInvalidScope
	}

	zones := make([]string, 0, len(s.ZoneIDs))

	for _, zone := range s.ZoneIDs {
		zone = strings.ToLower(strings.TrimSpace(zone))
		if !uuidShape.MatchString(zone) {
			return Scope{}, ErrInvalidScope
		}

		zones = append(zones, zone)
	}

	s.ZoneIDs = zones

	switch s.VehicleClass = strings.ToLower(strings.TrimSpace(s.VehicleClass)); s.VehicleClass {
	case "", "economy", "comfort":
	default:
		return Scope{}, ErrInvalidScope
	}

	if s.DailyStart < 0 || s.DailyStart > minutesInDay || s.DailyEnd < 0 || s.DailyEnd > minutesInDay {
		return Scope{}, ErrInvalidHours
	}

	s.TimeZone = strings.TrimSpace(s.TimeZone)
	if s.TimeZone == "" {
		s.TimeZone = "UTC"
	}

	if _, err := time.LoadLocation(s.TimeZone); err != nil {
		return Scope{}, ErrInvalidTimeZone
	}

	return s, nil
}
