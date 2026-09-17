package config

import (
	"fmt"
	"strings"
	"time"
)

// NATS holds the connection settings this service needs to consume
// events. Unlike the publishing services, notification-service never
// publishes to the outbox, so there is no PublishTimeout here.
type NATS struct {
	URL            string
	ClientName     string
	ConnectTimeout time.Duration
	ReconnectWait  time.Duration
	DrainTimeout   time.Duration
}

func ParseNATS(cfg Config) (NATS, error) {
	url := strings.TrimSpace(cfg.NATSURL)
	if url == "" {
		return NATS{}, fmt.Errorf("NATS_URL cannot be blank")
	}

	clientName := strings.TrimSpace(cfg.ServiceName)
	if clientName == "" {
		return NATS{}, fmt.Errorf("service name cannot be blank")
	}

	connectTimeout, err := parsePositiveDuration("NATS_CONNECT_TIMEOUT", cfg.NATSConnectTimeout)
	if err != nil {
		return NATS{}, err
	}

	reconnectWait, err := parsePositiveDuration("NATS_RECONNECT_WAIT", cfg.NATSReconnectWait)
	if err != nil {
		return NATS{}, err
	}

	drainTimeout, err := parsePositiveDuration("NATS_DRAIN_TIMEOUT", cfg.NATSDrainTimeout)
	if err != nil {
		return NATS{}, err
	}

	return NATS{
		URL:            url,
		ClientName:     clientName,
		ConnectTimeout: connectTimeout,
		ReconnectWait:  reconnectWait,
		DrainTimeout:   drainTimeout,
	}, nil
}

func parsePositiveDuration(name string, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s has invalid duration %q: %w", name, value, err)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}

	return duration, nil
}
