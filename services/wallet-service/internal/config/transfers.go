package config

import (
	"fmt"
	"strconv"
	"strings"
)

// TransferLookups is how many phones a rider may look up before sending, in
// an hour and in a day.
type TransferLookups struct {
	PerHour int
	PerDay  int
}

func ParseTransferLookups(cfg Config) (TransferLookups, error) {
	perHour, err := strconv.Atoi(strings.TrimSpace(cfg.TransferLookupMaxPerHour))
	if err != nil || perHour < 1 {
		return TransferLookups{}, fmt.Errorf("TRANSFER_LOOKUP_MAX_PER_HOUR must be a whole number of at least 1, got %q", cfg.TransferLookupMaxPerHour)
	}

	perDay, err := strconv.Atoi(strings.TrimSpace(cfg.TransferLookupMaxPerDay))
	if err != nil || perDay < perHour {
		return TransferLookups{}, fmt.Errorf("TRANSFER_LOOKUP_MAX_PER_DAY must be a whole number of at least TRANSFER_LOOKUP_MAX_PER_HOUR (%d), got %q", perHour, cfg.TransferLookupMaxPerDay)
	}

	return TransferLookups{PerHour: perHour, PerDay: perDay}, nil
}
