package nats

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func validConnectionTestConfig() ConnectionConfig {
	return ConnectionConfig{
		URL:            "nats://127.0.0.1:4222",
		ClientName:     "identity-service",
		ConnectTimeout: 5 * time.Second,
		ReconnectWait:  time.Second,
		DrainTimeout:   10 * time.Second,
	}
}

func connectionTestLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)
}

func TestValidateConnectionConfigAcceptsValidConfiguration(
	t *testing.T,
) {
	config := validConnectionTestConfig()

	if err := validateConnectionConfig(config); err != nil {
		t.Fatalf(
			"validateConnectionConfig() returned an error: %v",
			err,
		)
	}
}

func TestValidateConnectionConfigRejectsInvalidConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*ConnectionConfig)
	}{
		{
			name: "blank URL",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.URL = ""
			},
		},
		{
			name: "whitespace URL",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.URL = "   "
			},
		},
		{
			name: "untrimmed URL",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.URL =
					" nats://127.0.0.1:4222 "
			},
		},
		{
			name: "blank client name",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ClientName = ""
			},
		},
		{
			name: "whitespace client name",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ClientName = "   "
			},
		},
		{
			name: "untrimmed client name",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ClientName =
					" identity-service "
			},
		},
		{
			name: "zero connect timeout",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ConnectTimeout = 0
			},
		},
		{
			name: "negative connect timeout",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ConnectTimeout =
					-time.Second
			},
		},
		{
			name: "zero reconnect wait",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ReconnectWait = 0
			},
		},
		{
			name: "negative reconnect wait",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.ReconnectWait =
					-time.Second
			},
		},
		{
			name: "zero drain timeout",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.DrainTimeout = 0
			},
		},
		{
			name: "negative drain timeout",
			mutate: func(
				config *ConnectionConfig,
			) {
				config.DrainTimeout =
					-time.Second
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				config :=
					validConnectionTestConfig()

				testCase.mutate(
					&config,
				)

				err :=
					validateConnectionConfig(
						config,
					)

				if err == nil {
					t.Fatal(
						"validateConnectionConfig() accepted invalid configuration",
					)
				}
			},
		)
	}
}

func TestOpenConnectionPanicsForNilLogger(
	t *testing.T,
) {
	defer func() {
		if recover() == nil {
			t.Fatal(
				"OpenConnection() did not panic",
			)
		}
	}()

	_, _ = OpenConnection(
		validConnectionTestConfig(),
		nil,
	)
}

func TestOpenConnectionRejectsInvalidConfigurationBeforeConnecting(
	t *testing.T,
) {
	config := validConnectionTestConfig()
	config.URL = "   "

	connection, err := OpenConnection(
		config,
		connectionTestLogger(),
	)

	if err == nil {
		t.Fatal(
			"OpenConnection() returned nil error for invalid configuration",
		)
	}

	if connection != nil {
		t.Fatal(
			"OpenConnection() returned a connection for invalid configuration",
		)
	}
}

func TestNilConnectionJetStreamReturnsNil(
	t *testing.T,
) {
	var connection *Connection

	if connection.JetStream() != nil {
		t.Fatal(
			"JetStream() returned non-nil client for nil connection",
		)
	}
}

func TestNilConnectionDrainReturnsNil(
	t *testing.T,
) {
	var connection *Connection

	if err := connection.Drain(); err != nil {
		t.Fatalf(
			"Drain() returned an error for nil connection: %v",
			err,
		)
	}
}

func TestConnectionWithoutNATSConnectionDrainReturnsNil(
	t *testing.T,
) {
	connection := &Connection{}

	if err := connection.Drain(); err != nil {
		t.Fatalf(
			"Drain() returned an error for empty connection: %v",
			err,
		)
	}
}
