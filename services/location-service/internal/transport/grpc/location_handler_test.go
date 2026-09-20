package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// handlerFailingLocations fails GetLocation with a fixed error. The embedded
// interface is left nil: a test that reaches any other method should panic.
type handlerFailingLocations struct {
	location.Service
	err error
}

func (s handlerFailingLocations) GetLocation(context.Context, location.EntityType, string) (location.Location, error) {
	return location.Location{}, s.err
}

type handlerFailingZones struct {
	zone.Service
	err error
}

func (s handlerFailingZones) GetZone(context.Context, string) (zone.Zone, error) {
	return zone.Zone{}, s.err
}

func newHandlerFailingWith(err error) *LocationHandler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewLocationHandler(handlerFailingLocations{err: err}, handlerFailingZones{err: err}, logger)
}

// The handler used to drop its logger, so the first error it could not
// classify crashed the service with a nil pointer instead of returning Internal.
func TestUnclassifiedLocationErrorIsReportedNotPanicked(t *testing.T) {
	handler := newHandlerFailingWith(errors.New("valkey exploded"))

	_, err := handler.GetLocation(context.Background(), &locationv1.GetLocationRequest{
		EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER,
		EntityId:   "driver-a",
	})

	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal, got %v", status.Code(err))
	}
}

func TestUnclassifiedZoneErrorIsReportedNotPanicked(t *testing.T) {
	handler := newHandlerFailingWith(errors.New("postgres exploded"))

	_, err := handler.GetZone(context.Background(), &locationv1.GetZoneRequest{ZoneId: "zone-1"})

	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal, got %v", status.Code(err))
	}
}

func TestKnownLocationErrorsKeepTheirCodes(t *testing.T) {
	handler := newHandlerFailingWith(location.ErrLocationNotFound)

	_, err := handler.GetLocation(context.Background(), &locationv1.GetLocationRequest{
		EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER,
		EntityId:   "driver-a",
	})

	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", status.Code(err))
	}
}
