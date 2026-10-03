package clients

import (
	"context"
	"errors"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/incentives"
)

const activityPageSize = 500

// TripActivity counts drivers' trips for incentive campaigns (trip-service,
// internal token).
type TripActivity struct {
	client tripv1.TripServiceClient
}

var _ incentives.Trips = (*TripActivity)(nil)

func NewTripActivity(conn grpc.ClientConnInterface) *TripActivity {
	if conn == nil {
		panic("trip-service connection is required")
	}

	return &TripActivity{client: tripv1.NewTripServiceClient(conn)}
}

func scopeOf(c incentives.Campaign) *tripv1.ActivityScope {
	return &tripv1.ActivityScope{
		From:             timestamppb.New(c.StartsAt),
		To:               timestamppb.New(c.EndsAt),
		CityId:           c.CityID,
		ZoneIds:          c.ZoneIDs,
		VehicleClass:     c.VehicleClass,
		DailyStartMinute: int32(c.DailyStart),
		DailyEndMinute:   int32(c.DailyEnd),
		TimeZone:         c.TimeZone,
	}
}

func activityOf(a *tripv1.DriverActivity) incentives.Activity {
	return incentives.Activity{
		DriverID:            a.GetDriverId(),
		CompletedTrips:      int(a.GetCompletedTrips()),
		OffersAccepted:      int(a.GetOffersAccepted()),
		OffersDeclined:      int(a.GetOffersDeclined()),
		DriverCancellations: int(a.GetDriverCancellations()),
	}
}

func (t *TripActivity) Driver(ctx context.Context, driverID string, c incentives.Campaign) (incentives.Activity, error) {
	response, err := t.client.GetDriverActivity(ctx, &tripv1.GetDriverActivityRequest{DriverId: driverID, Scope: scopeOf(c)})
	if err != nil {
		return incentives.Activity{}, fmt.Errorf("call trip-service GetDriverActivity: %w", err)
	}

	return activityOf(response), nil
}

func (t *TripActivity) Drivers(ctx context.Context, c incentives.Campaign, minCompleted int, pageToken string) ([]incentives.Activity, string, error) {
	response, err := t.client.ListDriverActivity(ctx, &tripv1.ListDriverActivityRequest{
		Scope: scopeOf(c), MinCompletedTrips: int32(minCompleted), PageSize: activityPageSize, PageToken: pageToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("call trip-service ListDriverActivity: %w", err)
	}

	page := make([]incentives.Activity, 0, len(response.GetDrivers()))
	for _, a := range response.GetDrivers() {
		page = append(page, activityOf(a))
	}

	return page, response.GetNextPageToken(), nil
}

// DriverProfiles reads drivers' class and rating (driver-service, internal
// token).
type DriverProfiles struct {
	client driverv1.DriverServiceClient
}

var _ incentives.Drivers = (*DriverProfiles)(nil)

func NewDriverProfiles(conn grpc.ClientConnInterface) *DriverProfiles {
	if conn == nil {
		panic("driver-service connection is required")
	}

	return &DriverProfiles{client: driverv1.NewDriverServiceClient(conn)}
}

func (d *DriverProfiles) Profile(ctx context.Context, driverID string) (incentives.Profile, error) {
	response, err := d.client.GetDriver(ctx, &driverv1.GetDriverRequest{DriverId: driverID})
	if status.Code(err) == codes.NotFound {
		return incentives.Profile{}, incentives.ErrDriverNotFound
	}

	if err != nil {
		return incentives.Profile{}, fmt.Errorf("call driver-service GetDriver: %w", err)
	}

	found := response.GetDriver()
	if found == nil {
		return incentives.Profile{}, errors.New("driver-service returned no driver")
	}

	profile := incentives.Profile{VehicleClass: found.GetVehicle().GetVehicleClass()}

	if found.GetRatingCount() > 0 {
		rating := decimal.NewFromFloat(found.GetRatingAverage()).Round(2)
		profile.Rating = &rating
	}

	return profile, nil
}
