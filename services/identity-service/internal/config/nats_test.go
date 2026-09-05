package config

import (
	"testing"
	"time"
)

func validNATSTestConfig() Config {
	return Config{
		ServiceName: "identity-service",

		NATSURL:            "nats://127.0.0.1:4222",
		NATSPublishTimeout: "5s",
		NATSConnectTimeout: "5s",
		NATSReconnectWait:  "2s",
		NATSDrainTimeout:   "10s",
	}
}

func TestParseNATSReturnsParsedValues(
	t *testing.T,
) {
	cfg := validNATSTestConfig()

	cfg.NATSURL =
		"  nats://127.0.0.1:4222  "

	cfg.ServiceName =
		"  identity-service  "

	natsConfig, err := ParseNATS(cfg)
	if err != nil {
		t.Fatalf(
			"ParseNATS() returned an error: %v",
			err,
		)
	}

	if natsConfig.URL !=
		"nats://127.0.0.1:4222" {
		t.Fatalf(
			"URL = %q, expected %q",
			natsConfig.URL,
			"nats://127.0.0.1:4222",
		)
	}

	if natsConfig.ClientName !=
		"identity-service" {
		t.Fatalf(
			"ClientName = %q, expected %q",
			natsConfig.ClientName,
			"identity-service",
		)
	}

	if natsConfig.PublishTimeout !=
		5*time.Second {
		t.Fatalf(
			"PublishTimeout = %v, expected %v",
			natsConfig.PublishTimeout,
			5*time.Second,
		)
	}

	if natsConfig.ConnectTimeout !=
		5*time.Second {
		t.Fatalf(
			"ConnectTimeout = %v, expected %v",
			natsConfig.ConnectTimeout,
			5*time.Second,
		)
	}

	if natsConfig.ReconnectWait !=
		2*time.Second {
		t.Fatalf(
			"ReconnectWait = %v, expected %v",
			natsConfig.ReconnectWait,
			2*time.Second,
		)
	}

	if natsConfig.DrainTimeout !=
		10*time.Second {
		t.Fatalf(
			"DrainTimeout = %v, expected %v",
			natsConfig.DrainTimeout,
			10*time.Second,
		)
	}
}

func TestParseNATSRejectsBlankURL(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSURL = "   "

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted blank NATS_URL",
		)
	}
}

func TestParseNATSRejectsBlankServiceName(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.ServiceName = "   "

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted blank service name",
		)
	}
}

func TestParseNATSRejectsInvalidPublishTimeout(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSPublishTimeout = "invalid"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted invalid NATS_PUBLISH_TIMEOUT",
		)
	}
}

func TestParseNATSRejectsNonPositivePublishTimeout(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSPublishTimeout = "0s"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted non-positive NATS_PUBLISH_TIMEOUT",
		)
	}
}

func TestParseNATSRejectsInvalidConnectTimeout(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSConnectTimeout = "invalid"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted invalid NATS_CONNECT_TIMEOUT",
		)
	}
}

func TestParseNATSRejectsNonPositiveConnectTimeout(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSConnectTimeout = "0s"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted non-positive NATS_CONNECT_TIMEOUT",
		)
	}
}

func TestParseNATSRejectsInvalidReconnectWait(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSReconnectWait = "invalid"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted invalid NATS_RECONNECT_WAIT",
		)
	}
}

func TestParseNATSRejectsNonPositiveReconnectWait(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSReconnectWait = "0s"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted non-positive NATS_RECONNECT_WAIT",
		)
	}
}

func TestParseNATSRejectsInvalidDrainTimeout(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSDrainTimeout = "invalid"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted invalid NATS_DRAIN_TIMEOUT",
		)
	}
}

func TestParseNATSRejectsNonPositiveDrainTimeout(
	t *testing.T,
) {
	cfg := validNATSTestConfig()
	cfg.NATSDrainTimeout = "0s"

	_, err := ParseNATS(cfg)
	if err == nil {
		t.Fatal(
			"ParseNATS() accepted non-positive NATS_DRAIN_TIMEOUT",
		)
	}
}
