package config

import (
	"testing"
	"time"
)

func TestAccountDeletionDefaults(t *testing.T) {
	got, err := LoadAccountDeletion()
	if err != nil {
		t.Fatal(err)
	}

	if got.GracePeriod != 30*24*time.Hour || got.CheckInterval != time.Minute || got.RetryAfter != time.Hour {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestAccountDeletionRefusesAShortGracePeriod(t *testing.T) {
	t.Setenv("ACCOUNT_DELETION_GRACE_PERIOD", "1h")

	if _, err := LoadAccountDeletion(); err == nil {
		t.Fatal("a grace period under a day was accepted")
	}
}
