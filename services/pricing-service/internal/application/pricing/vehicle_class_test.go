package pricing

import (
	"errors"
	"testing"
)

func TestNormalizeVehicleClass(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "empty defaults to economy", raw: "", want: VehicleClassEconomy},
		{name: "whitespace only defaults to economy", raw: "   ", want: VehicleClassEconomy},
		{name: "economy", raw: "economy", want: VehicleClassEconomy},
		{name: "comfort", raw: "comfort", want: VehicleClassComfort},
		{name: "case and whitespace are normalised", raw: " Comfort ", want: VehicleClassComfort},
		{name: "unknown class is rejected", raw: "luxury", wantErr: ErrInvalidVehicleClass},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeVehicleClass(tc.raw)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}

			if got != tc.want {
				t.Errorf("class = %q, want %q", got, tc.want)
			}
		})
	}
}
