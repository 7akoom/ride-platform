package trip

import (
	"context"
	"errors"
	"strings"
)

// ErrRiderProfileUnavailable: the trip's rider profile cannot be read.
var ErrRiderProfileUnavailable = errors.New("the rider's profile is not available")

// RiderSummary is what a driver may know about the rider of their trip: what to call
// them and how other drivers rated them. No phone number, no identity, nothing about
// the rider's account.
type RiderSummary struct {
	FirstName     string
	RatingAverage float64
	RatingCount   int32
}

// RiderDirectory is the outbound port that reads a rider's profile from rider-service.
// DisplayName is the full name; the driver gets its first word only.
type RiderDirectory interface {
	RiderProfile(ctx context.Context, riderID string) (RiderProfile, error)
}

// RiderProfile is the part of a rider's profile the directory reads.
type RiderProfile struct {
	DisplayName   string
	RatingAverage float64
	RatingCount   int32
}

// WithRiderProfile decorates a Service with GetTripRider, the driver's view of who they
// are picking up. The transport layer reaches it through As, like GetTripDriver.
func WithRiderProfile(base Service, directory RiderDirectory) Service {
	if base == nil {
		panic("trip service is required")
	}

	if directory == nil {
		panic("rider directory is required")
	}

	return &riderProfileService{Service: base, directory: directory}
}

type riderProfileService struct {
	Service

	directory RiderDirectory
}

// Unwrap returns the Service the decorator wraps, so As can keep looking through it.
func (s *riderProfileService) Unwrap() Service { return s.Service }

// GetTripRider describes the rider of the trip. Who may ask (the driver of the trip) is
// decided before this is called.
func (s *riderProfileService) GetTripRider(ctx context.Context, tripID string) (RiderSummary, error) {
	found, err := s.Service.GetTrip(ctx, tripID)
	if err != nil {
		return RiderSummary{}, err
	}

	profile, err := s.directory.RiderProfile(ctx, found.RiderID)
	if err != nil {
		return RiderSummary{}, err
	}

	return RiderSummary{
		FirstName:     FirstName(profile.DisplayName),
		RatingAverage: profile.RatingAverage,
		RatingCount:   profile.RatingCount,
	}, nil
}

// FirstName is the first word of a name ("" for none).
func FirstName(displayName string) string {
	fields := strings.Fields(displayName)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}
