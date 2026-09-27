package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	// Time zones are compiled in: the container image has no zoneinfo.
	_ "time/tzdata"
)

const (
	maxReminderDays       = 365
	minDocumentsInterval  = 10 * time.Second
	maxDocumentsReminders = 10
)

type Documents struct {
	Location      *time.Location
	ReminderDays  []int
	CheckInterval time.Duration
}

func ParseDocuments(cfg Config) (Documents, error) {
	location, err := time.LoadLocation(strings.TrimSpace(cfg.DocumentsTimeZone))
	if err != nil || strings.TrimSpace(cfg.DocumentsTimeZone) == "" {
		return Documents{}, fmt.Errorf("DRIVER_DOCUMENTS_TIME_ZONE %q is not an IANA time zone", cfg.DocumentsTimeZone)
	}

	var days []int

	for _, part := range strings.Split(cfg.DocumentsReminderDays, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		day, err := strconv.Atoi(part)
		if err != nil || day < 1 || day > maxReminderDays {
			return Documents{}, fmt.Errorf("DRIVER_DOCUMENTS_REMINDER_DAYS must be whole days between 1 and %d, got %q", maxReminderDays, part)
		}

		days = append(days, day)
	}

	slices.Sort(days)
	days = slices.Compact(days)

	if len(days) > maxDocumentsReminders {
		return Documents{}, fmt.Errorf("DRIVER_DOCUMENTS_REMINDER_DAYS has more than %d reminders", maxDocumentsReminders)
	}

	interval, err := time.ParseDuration(strings.TrimSpace(cfg.DocumentsCheckInterval))
	if err != nil || interval < minDocumentsInterval {
		return Documents{}, fmt.Errorf("DRIVER_DOCUMENTS_CHECK_INTERVAL must be a duration of at least %s, got %q", minDocumentsInterval, cfg.DocumentsCheckInterval)
	}

	return Documents{Location: location, ReminderDays: days, CheckInterval: interval}, nil
}
