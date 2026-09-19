package config

import "time"

// AutoFare controls how the trip.completed consumer behaves when a fare
// can't be calculated on the first attempt.
type AutoFare struct {
	// RetryInterval is the pause between attempts when something
	// transient failed (trip-service unreachable, database hiccup, ...).
	RetryInterval time.Duration

	// GiveUpAfter is how long, measured from the moment the trip was
	// completed, the consumer keeps retrying before it logs an error and
	// stops. Bounded on purpose: a permanently failing trip must not
	// retry forever.
	GiveUpAfter time.Duration
}

func ParseAutoFare(cfg Config) (AutoFare, error) {
	retryInterval, err := parsePositiveDuration("FARE_RETRY_INTERVAL", cfg.FareRetryInterval)
	if err != nil {
		return AutoFare{}, err
	}

	giveUpAfter, err := parsePositiveDuration("FARE_GIVE_UP_AFTER", cfg.FareGiveUpAfter)
	if err != nil {
		return AutoFare{}, err
	}

	return AutoFare{
		RetryInterval: retryInterval,
		GiveUpAfter:   giveUpAfter,
	}, nil
}
