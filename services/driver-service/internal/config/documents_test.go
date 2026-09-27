package config

import (
	"slices"
	"testing"
	"time"
)

func TestParseDocuments(t *testing.T) {
	got, err := ParseDocuments(Config{
		DocumentsTimeZone:      "Asia/Baghdad",
		DocumentsReminderDays:  " 7, 30,1,7 ",
		DocumentsCheckInterval: "2m",
	})
	if err != nil {
		t.Fatalf("ParseDocuments: %v", err)
	}

	if got.Location.String() != "Asia/Baghdad" || got.CheckInterval != 2*time.Minute {
		t.Errorf("got %v / %v", got.Location, got.CheckInterval)
	}

	if !slices.Equal(got.ReminderDays, []int{1, 7, 30}) {
		t.Errorf("reminder days = %v, want sorted and without repeats", got.ReminderDays)
	}

	none, err := ParseDocuments(Config{DocumentsTimeZone: "UTC", DocumentsCheckInterval: "1m"})
	if err != nil || len(none.ReminderDays) != 0 {
		t.Errorf("no reminders: %v, %v", none.ReminderDays, err)
	}
}

func TestParseDocuments_Refuses(t *testing.T) {
	bad := []Config{
		{DocumentsTimeZone: "Mars/Olympus", DocumentsReminderDays: "7", DocumentsCheckInterval: "1m"},
		{DocumentsTimeZone: "", DocumentsReminderDays: "7", DocumentsCheckInterval: "1m"},
		{DocumentsTimeZone: "UTC", DocumentsReminderDays: "0", DocumentsCheckInterval: "1m"},
		{DocumentsTimeZone: "UTC", DocumentsReminderDays: "seven", DocumentsCheckInterval: "1m"},
		{DocumentsTimeZone: "UTC", DocumentsReminderDays: "400", DocumentsCheckInterval: "1m"},
		{DocumentsTimeZone: "UTC", DocumentsReminderDays: "7", DocumentsCheckInterval: "1s"},
		{DocumentsTimeZone: "UTC", DocumentsReminderDays: "7", DocumentsCheckInterval: "soon"},
	}

	for _, cfg := range bad {
		if _, err := ParseDocuments(cfg); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
}
