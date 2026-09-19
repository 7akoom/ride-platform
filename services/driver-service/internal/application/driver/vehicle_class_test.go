package driver_test

import (
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

func TestNewVehicleClass(t *testing.T) {
	cases := []struct {
		name      string
		class     string
		wantClass driver.VehicleClass
		wantErr   error
	}{
		{name: "empty means not specified", class: "", wantClass: ""},
		{name: "economy", class: "economy", wantClass: driver.VehicleClassEconomy},
		{name: "comfort", class: "comfort", wantClass: driver.VehicleClassComfort},
		{name: "case and whitespace are normalised", class: "  Comfort ", wantClass: driver.VehicleClassComfort},
		{name: "unknown class is rejected", class: "luxury", wantErr: driver.ErrInvalidVehicleClass},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := driver.NewVehicle("Toyota", "Corolla", "White", "ABC123", tc.class)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}

			if err != nil {
				return
			}

			if got.Class != tc.wantClass {
				t.Errorf("Class = %q, want %q", got.Class, tc.wantClass)
			}
		})
	}
}

func TestNewVehicleRequiredFieldsCheckedBeforeClass(t *testing.T) {
	_, err := driver.NewVehicle("", "Corolla", "White", "ABC123", "luxury")

	if !errors.Is(err, driver.ErrVehicleFieldsRequired) {
		t.Fatalf("error = %v, want ErrVehicleFieldsRequired", err)
	}
}
