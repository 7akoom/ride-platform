package grpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/share"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"github.com/7akoom/ride-platform/services/trip-service/internal/infrastructure/token"
)

func TestOnlyTheRiderSharesTheirTrip(t *testing.T) {
	for _, method := range []string{"ShareTrip", "StopSharingTrip"} {
		var request any = &tripv1.ShareTripRequest{TripId: "trip-1"}
		if method == "StopSharingTrip" {
			request = &tripv1.StopSharingTripRequest{TripId: "trip-1"}
		}

		runOwnershipCases(t, codes.OK, []ownershipCase{{"the rider: " + method, "id-rider-a", method, request}})
		runOwnershipCases(t, codes.PermissionDenied, []ownershipCase{
			{"the driver: " + method, "id-driver-a", method, request},
			{"a stranger: " + method, "id-rider-b", method, request},
		})
	}
}

func TestASharedTripNeedsNoAccountAndNothingElseDoes(t *testing.T) {
	dir := t.TempDir()

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "public.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	verifier, err := token.NewAccessTokenVerifier(path, "issuer", "audience", "key")
	if err != nil {
		t.Fatal(err)
	}

	authenticate := NewAuthenticationUnaryInterceptor(verifier, "internal-secret")
	authorize := NewAuthorizationUnaryInterceptor(ownershipProfiles, ownershipTrips)

	call := func(method string) codes.Code {
		info := &googlegrpc.UnaryServerInfo{FullMethod: tripRPCPrefix + method}

		_, err := authenticate(context.Background(), nil, info, func(ctx context.Context, request any) (any, error) {
			return authorize(ctx, request, info, func(context.Context, any) (any, error) { return nil, nil })
		})

		return status.Code(err)
	}

	if got := call("GetSharedTrip"); got != codes.OK {
		t.Fatalf("a shared trip without a token: %v", got)
	}

	for _, method := range []string{"GetTrip", "ShareTrip", "GetDriverLocation"} {
		if got := call(method); got != codes.Unauthenticated {
			t.Errorf("%s without a token: %v", method, got)
		}
	}
}

func TestALinkShowsTheTripNotWhoRidesItNorWhatItCosts(t *testing.T) {
	updated := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	expires := updated.Add(time.Hour)

	out := toProtoSharedTrip(share.View{
		Trip: trip.Trip{
			ID: "trip-1", RiderID: "rider-1", DriverID: "driver-1", Status: trip.StatusInProgress,
			PickupAddress: "Home", DropoffAddress: "Airport", QuotedFare: "7500",
			PassengerName: "Sara", PassengerPhone: "+9647500000002",
			Stops: []trip.Stop{{Address: "Bakery"}},
		},
		Driver:    &trip.DriverSummary{DisplayName: "Karwan", PlateNumber: "12345", VehicleMake: "Kia"},
		Location:  &trip.DriverLocation{Latitude: 36.2, Longitude: 44.0, UpdatedAt: updated},
		ExpiresAt: expires,
	})

	if out.GetStatus() != tripv1.TripStatus_TRIP_STATUS_IN_PROGRESS || out.GetDropoffAddress() != "Airport" ||
		len(out.GetStops()) != 1 || out.GetDriverName() != "Karwan" || out.GetVehicle().GetPlateNumber() != "12345" ||
		out.GetDriverLocation().GetLatitude() != 36.2 || !out.GetDriverLocationUpdatedAt().AsTime().Equal(updated) ||
		!out.GetLinkExpiresAt().AsTime().Equal(expires) {
		t.Fatalf("out %+v", out)
	}

	// Nothing before a driver.
	if out := toProtoSharedTrip(share.View{Trip: trip.Trip{Status: trip.StatusRequested}}); out.GetVehicle() != nil || out.GetDriverLocation() != nil {
		t.Fatalf("before a driver %+v", out)
	}
}

func TestShareErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &TripHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		share.ErrNotFound:     codes.NotFound,
		share.ErrTripNotLive:  codes.FailedPrecondition,
		share.ErrTooManyLinks: codes.FailedPrecondition,
		trip.ErrTripNotFound:  codes.NotFound,
	} {
		if got := status.Code(handler.mapShareError(err)); got != want {
			t.Errorf("%v: got %v, want %v", err, got, want)
		}
	}

	if _, err := handler.GetSharedTrip(context.Background(), &tripv1.GetSharedTripRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("without shares: %v", err)
	}
}
