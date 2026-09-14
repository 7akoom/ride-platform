package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxOutboxBatchSize = 1000

type Outbox struct {
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	BatchSize         int
	InitialRetryDelay time.Duration
	MaxRetryDelay     time.Duration
}

func ParseOutbox(
	cfg Config,
) (Outbox, error) {
	pollInterval, err :=
		parsePositiveDuration(
			"OUTBOX_POLL_INTERVAL",
			cfg.OutboxPollInterval,
		)
	if err != nil {
		return Outbox{}, err
	}

	leaseDuration, err :=
		parsePositiveDuration(
			"OUTBOX_LEASE_DURATION",
			cfg.OutboxLeaseDuration,
		)
	if err != nil {
		return Outbox{}, err
	}

	batchSize, err := strconv.Atoi(
		strings.TrimSpace(
			cfg.OutboxBatchSize,
		),
	)
	if err != nil {
		return Outbox{}, fmt.Errorf(
			"OUTBOX_BATCH_SIZE has invalid integer %q: %w",
			cfg.OutboxBatchSize,
			err,
		)
	}

	if batchSize <= 0 {
		return Outbox{}, fmt.Errorf(
			"OUTBOX_BATCH_SIZE must be greater than zero",
		)
	}

	if batchSize > maxOutboxBatchSize {
		return Outbox{}, fmt.Errorf(
			"OUTBOX_BATCH_SIZE cannot exceed %d",
			maxOutboxBatchSize,
		)
	}

	initialRetryDelay, err :=
		parsePositiveDuration(
			"OUTBOX_INITIAL_RETRY_DELAY",
			cfg.OutboxInitialRetryDelay,
		)
	if err != nil {
		return Outbox{}, err
	}

	maxRetryDelay, err :=
		parsePositiveDuration(
			"OUTBOX_MAX_RETRY_DELAY",
			cfg.OutboxMaxRetryDelay,
		)
	if err != nil {
		return Outbox{}, err
	}

	if initialRetryDelay > maxRetryDelay {
		return Outbox{}, fmt.Errorf(
			"OUTBOX_INITIAL_RETRY_DELAY cannot exceed OUTBOX_MAX_RETRY_DELAY",
		)
	}

	return Outbox{
		PollInterval:      pollInterval,
		LeaseDuration:     leaseDuration,
		BatchSize:         batchSize,
		InitialRetryDelay: initialRetryDelay,
		MaxRetryDelay:     maxRetryDelay,
	}, nil
}
