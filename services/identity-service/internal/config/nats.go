package config

import (
	"fmt"
	"strings"
	"time"
)

type NATS struct {
	URL            string
	ClientName     string
	PublishTimeout time.Duration
	ConnectTimeout time.Duration
	ReconnectWait  time.Duration
	DrainTimeout   time.Duration
}

func ParseNATS(
	cfg Config,
) (NATS, error) {
	url := strings.TrimSpace(
		cfg.NATSURL,
	)
	if url == "" {
		return NATS{}, fmt.Errorf(
			"NATS_URL cannot be blank",
		)
	}

	clientName := strings.TrimSpace(
		cfg.ServiceName,
	)
	if clientName == "" {
		return NATS{}, fmt.Errorf(
			"service name cannot be blank",
		)
	}

	publishTimeout, err :=
		parsePositiveDuration(
			"NATS_PUBLISH_TIMEOUT",
			cfg.NATSPublishTimeout,
		)
	if err != nil {
		return NATS{}, err
	}

	connectTimeout, err :=
		parsePositiveDuration(
			"NATS_CONNECT_TIMEOUT",
			cfg.NATSConnectTimeout,
		)
	if err != nil {
		return NATS{}, err
	}

	reconnectWait, err :=
		parsePositiveDuration(
			"NATS_RECONNECT_WAIT",
			cfg.NATSReconnectWait,
		)
	if err != nil {
		return NATS{}, err
	}

	drainTimeout, err :=
		parsePositiveDuration(
			"NATS_DRAIN_TIMEOUT",
			cfg.NATSDrainTimeout,
		)
	if err != nil {
		return NATS{}, err
	}

	return NATS{
		URL:            url,
		ClientName:     clientName,
		PublishTimeout: publishTimeout,
		ConnectTimeout: connectTimeout,
		ReconnectWait:  reconnectWait,
		DrainTimeout:   drainTimeout,
	}, nil
}
