package trip_test

import (
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestNormalizeVehicleClass(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "empty defaults to economy", raw: "", want: trip.VehicleClassEconomy},
		{name: "whitespace only defaults to economy", raw: "  ", want: trip.VehicleClassEconomy},
		{name: "economy", raw: "economy", want: trip.VehicleClassEconomy},
		{name: "comfort", raw: "comfort", want: trip.VehicleClassComfort},
		{name: "case and whitespace are normalised", raw: " Comfort ", want: trip.VehicleClassComfort},
		{name: "unknown class is rejected", raw: "luxury", wantErr: trip.ErrInvalidVehicleClass},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := trip.NormalizeVehicleClass(tc.raw)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}

			if got != tc.want {
				t.Errorf("class = %q, want %q", got, tc.want)
			}
		})
	}
}
