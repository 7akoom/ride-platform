package config

import "time"

// AutoDispatch controls how long the trip.requested consumer keeps
// looking for a driver on a trip's behalf.
type AutoDispatch struct {
	// RetryInterval is the pause between attempts when no driver could
	// be assigned yet (nobody nearby, or every candidate was ineligible).
	RetryInterval time.Duration

	// SearchTimeout is how long, measured from the moment the trip was
	// requested, dispatch keeps retrying before giving up on it.
	SearchTimeout time.Duration
}

func ParseAutoDispatch(cfg Config) (AutoDispatch, error) {
	retryInterval, err := parsePositiveDuration("DISPATCH_RETRY_INTERVAL", cfg.DispatchRetryInterval)
	if err != nil {
		return AutoDispatch{}, err
	}

	searchTimeout, err := parsePositiveDuration("DISPATCH_SEARCH_TIMEOUT", cfg.DispatchSearchTimeout)
	if err != nil {
		return AutoDispatch{}, err
	}

	return AutoDispatch{
		RetryInterval: retryInterval,
		SearchTimeout: searchTimeout,
	}, nil
}
