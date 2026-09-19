package config

import (
	"fmt"
	"strings"
	"time"
)

// AutoSettle controls how the fare.calculated consumer settles a trip.
type AutoSettle struct {
	// RetryInterval is the pause between attempts when something
	// transient failed (trip-service unreachable, database hiccup, ...).
	RetryInterval time.Duration

	// GiveUpAfter is how long, measured from the moment the fare was
	// calculated, the consumer keeps retrying before it logs an error and
	// stops. Bounded on purpose: a permanently failing trip must not
	// retry forever.
	GiveUpAfter time.Duration

	// DefaultPaymentMethod (cash, wallet or card) is used for every
	// automatically settled trip, because a trip does not record how the
	// rider paid yet. cash is the safe default for a cash-dominant market:
	// it never touches a rider's wallet and draws the platform's commission
	// from the driver's prepaid balance.
	DefaultPaymentMethod string
}

func ParseAutoSettle(cfg Config) (AutoSettle, error) {
	retryInterval, err := parsePositiveDuration("SETTLEMENT_RETRY_INTERVAL", cfg.SettlementRetryInterval)
	if err != nil {
		return AutoSettle{}, err
	}

	giveUpAfter, err := parsePositiveDuration("SETTLEMENT_GIVE_UP_AFTER", cfg.SettlementGiveUpAfter)
	if err != nil {
		return AutoSettle{}, err
	}

	method := strings.ToLower(strings.TrimSpace(cfg.SettlementDefaultPaymentMethod))

	switch method {
	case "cash", "wallet", "card":
	default:
		return AutoSettle{}, fmt.Errorf(
			"SETTLEMENT_DEFAULT_PAYMENT_METHOD must be cash, wallet or card, got %q",
			cfg.SettlementDefaultPaymentMethod,
		)
	}

	return AutoSettle{
		RetryInterval:        retryInterval,
		GiveUpAfter:          giveUpAfter,
		DefaultPaymentMethod: method,
	}, nil
}
