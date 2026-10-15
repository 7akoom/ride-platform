package config

import "testing"

func TestParseTransferLookups(t *testing.T) {
	limits, err := ParseTransferLookups(Config{TransferLookupMaxPerHour: "30", TransferLookupMaxPerDay: " 100 "})
	if err != nil || limits.PerHour != 30 || limits.PerDay != 100 {
		t.Fatalf("limits %+v, %v", limits, err)
	}

	bad := []Config{
		{TransferLookupMaxPerHour: "0", TransferLookupMaxPerDay: "100"},
		{TransferLookupMaxPerHour: "x", TransferLookupMaxPerDay: "100"},
		{TransferLookupMaxPerHour: "30", TransferLookupMaxPerDay: "10"},
		{TransferLookupMaxPerHour: "30", TransferLookupMaxPerDay: ""},
	}

	for _, cfg := range bad {
		if _, err := ParseTransferLookups(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}
