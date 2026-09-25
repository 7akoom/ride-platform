package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is the deployment's rules for trips booked ahead.
type Schedule struct {
	MinAhead     time.Duration
	MaxAhead     time.Duration
	MaxUpcoming  int
	DispatchLead time.Duration
	Grace        time.Duration
	PollInterval time.Duration
}

// ParseSchedule reads the TRIP_SCHEDULE_* settings. The trip is requested
// DispatchLead before its time, so a booking must be further ahead than
// that.
func ParseSchedule(cfg Config) (Schedule, error) {
	var (
		out Schedule
		err error
	)

	durations := []struct {
		name  string
		value string
		into  *time.Duration
		min   time.Duration
	}{
		{"TRIP_SCHEDULE_MIN_AHEAD", cfg.ScheduleMinAhead, &out.MinAhead, 5 * time.Minute},
		{"TRIP_SCHEDULE_MAX_AHEAD", cfg.ScheduleMaxAhead, &out.MaxAhead, time.Hour},
		{"TRIP_SCHEDULE_DISPATCH_LEAD", cfg.ScheduleDispatchLead, &out.DispatchLead, time.Minute},
		{"TRIP_SCHEDULE_GRACE", cfg.ScheduleGrace, &out.Grace, time.Minute},
		{"TRIP_SCHEDULE_POLL_INTERVAL", cfg.SchedulePollInterval, &out.PollInterval, time.Second},
	}

	for _, d := range durations {
		if *d.into, err = time.ParseDuration(strings.TrimSpace(d.value)); err != nil || *d.into < d.min {
			return Schedule{}, fmt.Errorf("%s must be a duration of at least %s, got %q", d.name, d.min, d.value)
		}
	}

	if out.MaxUpcoming, err = strconv.Atoi(strings.TrimSpace(cfg.ScheduleMaxUpcoming)); err != nil || out.MaxUpcoming < 1 {
		return Schedule{}, fmt.Errorf("TRIP_SCHEDULE_MAX_UPCOMING must be a whole number of at least 1, got %q", cfg.ScheduleMaxUpcoming)
	}

	switch {
	case out.DispatchLead >= out.MinAhead:
		return Schedule{}, fmt.Errorf("TRIP_SCHEDULE_DISPATCH_LEAD (%s) must be shorter than TRIP_SCHEDULE_MIN_AHEAD (%s)", out.DispatchLead, out.MinAhead)
	case out.MaxAhead <= out.MinAhead:
		return Schedule{}, fmt.Errorf("TRIP_SCHEDULE_MAX_AHEAD must be longer than TRIP_SCHEDULE_MIN_AHEAD")
	}

	return out, nil
}
