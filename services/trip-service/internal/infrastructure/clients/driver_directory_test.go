package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// driverDirectoryFakeClient answers GetDriver and GetDriverPhoto only.
type driverDirectoryFakeClient struct {
	driverv1.DriverServiceClient

	driverErr error
	photo     *driverv1.DriverPhotoResponse
	photoErr  error
}

func (f *driverDirectoryFakeClient) GetDriver(
	context.Context, *driverv1.GetDriverRequest, ...grpc.CallOption,
) (*driverv1.GetDriverResponse, error) {
	if f.driverErr != nil {
		return nil, f.driverErr
	}

	return &driverv1.GetDriverResponse{Driver: &driverv1.Driver{
		DisplayName:   "Ali Hasan",
		RatingAverage: 4.8,
		RatingCount:   12,
		Vehicle:       &driverv1.Vehicle{Make: "Kia", Model: "Rio", Color: "Red", PlateNumber: "12345", VehicleClass: "economy"},
	}}, nil
}

func (f *driverDirectoryFakeClient) GetDriverPhoto(
	context.Context, *driverv1.GetDriverPhotoRequest, ...grpc.CallOption,
) (*driverv1.DriverPhotoResponse, error) {
	return f.photo, f.photoErr
}

func TestDriverSummaryCarriesThePhotoLink(t *testing.T) {
	expires := time.Date(2026, 10, 4, 12, 5, 0, 0, time.UTC)
	fake := &driverDirectoryFakeClient{photo: &driverv1.DriverPhotoResponse{Url: "https://files/p", ExpiresAt: timestamppb.New(expires)}}

	got, err := (&DriverDirectory{client: fake}).DriverSummary(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}

	if got.DisplayName != "Ali Hasan" || got.PlateNumber != "12345" || got.PhotoURL != "https://files/p" || !got.PhotoURLExpiresAt.Equal(expires) {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverSummaryWithoutAPhotoStillDescribesTheDriver(t *testing.T) {
	for _, photoErr := range []error{status.Error(codes.NotFound, "no photo"), status.Error(codes.Unavailable, "media down")} {
		fake := &driverDirectoryFakeClient{photoErr: photoErr}

		got, err := (&DriverDirectory{client: fake}).DriverSummary(context.Background(), "d1")
		if err != nil || got.DisplayName != "Ali Hasan" || got.PhotoURL != "" {
			t.Fatalf("%v: got %+v %v", photoErr, got, err)
		}
	}
}

func TestDriverSummaryOfAMissingDriver(t *testing.T) {
	fake := &driverDirectoryFakeClient{driverErr: status.Error(codes.NotFound, "gone")}

	if _, err := (&DriverDirectory{client: fake}).DriverSummary(context.Background(), "d1"); !errors.Is(err, trip.ErrDriverProfileUnavailable) {
		t.Fatalf("got %v", err)
	}
}
