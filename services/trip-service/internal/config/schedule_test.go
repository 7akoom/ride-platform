package config

import (
	"testing"
	"time"
)

func scheduleConfig() Config {
	return Config{
		ScheduleMinAhead: "30m", ScheduleMaxAhead: "168h", ScheduleMaxUpcoming: "3",
		ScheduleDispatchLead: "10m", ScheduleGrace: "10m", SchedulePollInterval: "15s",
	}
}

func TestParseSchedule(t *testing.T) {
	got, err := ParseSchedule(scheduleConfig())
	if err != nil || got.MinAhead != 30*time.Minute || got.MaxUpcoming != 3 || got.DispatchLead != 10*time.Minute {
		t.Fatalf("defaults %+v %v", got, err)
	}

	for name, edit := range map[string]func(*Config){
		"a lead as long as the least ahead": func(c *Config) { c.ScheduleDispatchLead = "30m" },
		"the most ahead under the least":    func(c *Config) { c.ScheduleMaxAhead = "20m" },
		"no upcoming allowed":               func(c *Config) { c.ScheduleMaxUpcoming = "0" },
		"not a duration":                    func(c *Config) { c.ScheduleGrace = "ten minutes" },
		"a poll under a second":             func(c *Config) { c.SchedulePollInterval = "10ms" },
	} {
		cfg := scheduleConfig()
		edit(&cfg)

		if _, err := ParseSchedule(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
