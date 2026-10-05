package postgres

import (
	"context"
	"testing"
)

func TestDriverSupplyCounts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	addDriver(t, pool, "available")
	addDriver(t, pool, "available")
	addDriver(t, pool, "offline")

	counts, err := NewDriverSupply(pool).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}

	by := map[string]int64{}
	for _, c := range counts {
		by[c.GetStatus()+"/"+c.GetAvailability()] += c.GetDrivers()
	}

	if by["active/available"] != 2 || by["active/offline"] != 1 {
		t.Fatalf("counts %v", by)
	}
}
