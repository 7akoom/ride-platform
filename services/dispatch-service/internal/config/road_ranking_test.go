package config

import (
	"testing"
	"time"
)

func TestRoadRankingDefaultsAndLimits(t *testing.T) {
	got, err := ParseRoadRanking(Config{})
	if err != nil || !got.Enabled || got.MaxPickup != 20*time.Minute {
		t.Fatalf("defaults: %+v %v", got, err)
	}

	got, err = ParseRoadRanking(Config{DispatchRoadRanking: "false", DispatchMaxPickupETA: "0"})
	if err != nil || got.Enabled || got.MaxPickup != 0 {
		t.Fatalf("off, no limit: %+v %v", got, err)
	}

	for _, bad := range []Config{
		{DispatchRoadRanking: "maybe"},
		{DispatchMaxPickupETA: "soon"},
		{DispatchMaxPickupETA: "-1m"},
		{DispatchMaxPickupETA: "3h"},
	} {
		if _, err := ParseRoadRanking(bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
