package config

import (
	"testing"
	"time"
)

func validOutboxTestConfig() Config {
	return Config{
		OutboxPollInterval:      "500ms",
		OutboxLeaseDuration:     "30s",
		OutboxBatchSize:         "100",
		OutboxInitialRetryDelay: "1s",
		OutboxMaxRetryDelay:     "1m",
	}
}

func TestParseOutboxReturnsParsedValues(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()

	cfg.OutboxBatchSize = " 100 "

	outboxConfig, err := ParseOutbox(cfg)
	if err != nil {
		t.Fatalf(
			"ParseOutbox() returned an error: %v",
			err,
		)
	}

	if outboxConfig.PollInterval !=
		500*time.Millisecond {
		t.Fatalf(
			"PollInterval = %v, expected %v",
			outboxConfig.PollInterval,
			500*time.Millisecond,
		)
	}

	if outboxConfig.LeaseDuration !=
		30*time.Second {
		t.Fatalf(
			"LeaseDuration = %v, expected %v",
			outboxConfig.LeaseDuration,
			30*time.Second,
		)
	}

	if outboxConfig.BatchSize != 100 {
		t.Fatalf(
			"BatchSize = %d, expected 100",
			outboxConfig.BatchSize,
		)
	}

	if outboxConfig.InitialRetryDelay !=
		time.Second {
		t.Fatalf(
			"InitialRetryDelay = %v, expected %v",
			outboxConfig.InitialRetryDelay,
			time.Second,
		)
	}

	if outboxConfig.MaxRetryDelay !=
		time.Minute {
		t.Fatalf(
			"MaxRetryDelay = %v, expected %v",
			outboxConfig.MaxRetryDelay,
			time.Minute,
		)
	}
}

func TestParseOutboxRejectsInvalidPollInterval(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxPollInterval = "invalid"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() accepted invalid OUTBOX_POLL_INTERVAL",
		)
	}
}

func TestParseOutboxRejectsNonPositivePollInterval(
	t *testing.T,
) {
	tests := []string{
		"0s",
		"-1s",
	}

	for _, value := range tests {
		t.Run(
			value,
			func(t *testing.T) {
				cfg := validOutboxTestConfig()
				cfg.OutboxPollInterval = value

				_, err := ParseOutbox(cfg)
				if err == nil {
					t.Fatalf(
						"ParseOutbox() accepted OUTBOX_POLL_INTERVAL=%q",
						value,
					)
				}
			},
		)
	}
}

func TestParseOutboxRejectsInvalidLeaseDuration(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxLeaseDuration = "invalid"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() accepted invalid OUTBOX_LEASE_DURATION",
		)
	}
}

func TestParseOutboxRejectsNonPositiveLeaseDuration(
	t *testing.T,
) {
	tests := []string{
		"0s",
		"-1s",
	}

	for _, value := range tests {
		t.Run(
			value,
			func(t *testing.T) {
				cfg := validOutboxTestConfig()
				cfg.OutboxLeaseDuration = value

				_, err := ParseOutbox(cfg)
				if err == nil {
					t.Fatalf(
						"ParseOutbox() accepted OUTBOX_LEASE_DURATION=%q",
						value,
					)
				}
			},
		)
	}
}

func TestParseOutboxRejectsInvalidBatchSize(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxBatchSize = "abc"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() accepted invalid OUTBOX_BATCH_SIZE",
		)
	}
}

func TestParseOutboxRejectsNonPositiveBatchSize(
	t *testing.T,
) {
	tests := []string{
		"0",
		"-1",
	}

	for _, value := range tests {
		t.Run(
			value,
			func(t *testing.T) {
				cfg := validOutboxTestConfig()
				cfg.OutboxBatchSize = value

				_, err := ParseOutbox(cfg)
				if err == nil {
					t.Fatalf(
						"ParseOutbox() accepted OUTBOX_BATCH_SIZE=%q",
						value,
					)
				}
			},
		)
	}
}

func TestParseOutboxRejectsBatchSizeAboveMaximum(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxBatchSize = "1001"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() accepted OUTBOX_BATCH_SIZE above maximum",
		)
	}
}

func TestParseOutboxAllowsMaximumBatchSize(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxBatchSize = "1000"

	outboxConfig, err := ParseOutbox(cfg)
	if err != nil {
		t.Fatalf(
			"ParseOutbox() rejected maximum batch size: %v",
			err,
		)
	}

	if outboxConfig.BatchSize != 1000 {
		t.Fatalf(
			"BatchSize = %d, expected 1000",
			outboxConfig.BatchSize,
		)
	}
}

func TestParseOutboxRejectsInvalidInitialRetryDelay(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxInitialRetryDelay = "invalid"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() accepted invalid OUTBOX_INITIAL_RETRY_DELAY",
		)
	}
}

func TestParseOutboxRejectsNonPositiveInitialRetryDelay(
	t *testing.T,
) {
	tests := []string{
		"0s",
		"-1s",
	}

	for _, value := range tests {
		t.Run(
			value,
			func(t *testing.T) {
				cfg := validOutboxTestConfig()
				cfg.OutboxInitialRetryDelay = value

				_, err := ParseOutbox(cfg)
				if err == nil {
					t.Fatalf(
						"ParseOutbox() accepted OUTBOX_INITIAL_RETRY_DELAY=%q",
						value,
					)
				}
			},
		)
	}
}

func TestParseOutboxRejectsInvalidMaxRetryDelay(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()
	cfg.OutboxMaxRetryDelay = "invalid"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() accepted invalid OUTBOX_MAX_RETRY_DELAY",
		)
	}
}

func TestParseOutboxRejectsNonPositiveMaxRetryDelay(
	t *testing.T,
) {
	tests := []string{
		"0s",
		"-1s",
	}

	for _, value := range tests {
		t.Run(
			value,
			func(t *testing.T) {
				cfg := validOutboxTestConfig()
				cfg.OutboxMaxRetryDelay = value

				_, err := ParseOutbox(cfg)
				if err == nil {
					t.Fatalf(
						"ParseOutbox() accepted OUTBOX_MAX_RETRY_DELAY=%q",
						value,
					)
				}
			},
		)
	}
}

func TestParseOutboxRejectsInitialRetryDelayGreaterThanMaximum(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()

	cfg.OutboxInitialRetryDelay = "2m"
	cfg.OutboxMaxRetryDelay = "1m"

	_, err := ParseOutbox(cfg)
	if err == nil {
		t.Fatal(
			"ParseOutbox() allowed initial retry delay greater than maximum",
		)
	}
}

func TestParseOutboxAllowsInitialRetryDelayEqualToMaximum(
	t *testing.T,
) {
	cfg := validOutboxTestConfig()

	cfg.OutboxInitialRetryDelay = "30s"
	cfg.OutboxMaxRetryDelay = "30s"

	outboxConfig, err := ParseOutbox(cfg)
	if err != nil {
		t.Fatalf(
			"ParseOutbox() rejected equal retry delays: %v",
			err,
		)
	}

	if outboxConfig.InitialRetryDelay !=
		30*time.Second {
		t.Fatalf(
			"InitialRetryDelay = %v, expected %v",
			outboxConfig.InitialRetryDelay,
			30*time.Second,
		)
	}

	if outboxConfig.MaxRetryDelay !=
		30*time.Second {
		t.Fatalf(
			"MaxRetryDelay = %v, expected %v",
			outboxConfig.MaxRetryDelay,
			30*time.Second,
		)
	}
}
