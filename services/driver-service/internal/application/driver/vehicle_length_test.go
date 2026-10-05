package driver_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

func TestNewVehicleRefusesFieldsTooLongToStore(t *testing.T) {
	long := func(n int) string { return strings.Repeat("ب", n) }

	for name, fields := range map[string][4]string{
		"plate": {"Kia", "Rio", "Blue", "E2E-METRICS-1759676190"},
		"make":  {long(61), "Rio", "Blue", "A1"},
		"model": {"Kia", long(61), "Blue", "A1"},
		"color": {"Kia", "Rio", long(41), "A1"},
	} {
		if _, err := driver.NewVehicle(fields[0], fields[1], fields[2], fields[3], "economy"); !errors.Is(err, driver.ErrVehicleFieldsTooLong) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	if _, err := driver.NewVehicle(long(60), long(60), long(40), "ABCDEFGHIJ0123456789", "economy"); err != nil {
		t.Errorf("at the limits: %v", err)
	}
}
