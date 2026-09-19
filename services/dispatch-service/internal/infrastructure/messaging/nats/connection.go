package nats

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const unlimitedReconnectAttempts = -1

type ConnectionConfig struct {
	URL            string
	ClientName     string
	ConnectTimeout time.Duration
	ReconnectWait  time.Duration
	DrainTimeout   time.Duration
}

type Connection struct {
	conn      *natsgo.Conn
	jetStream jetstream.JetStream
}

func OpenConnection(
	config ConnectionConfig,
	logger *slog.Logger,
) (*Connection, error) {
	if logger == nil {
		panic("NATS logger is required")
	}

	if err := validateConnectionConfig(config); err != nil {
		return nil, err
	}

	conn, err := natsgo.Connect(
		config.URL,

		natsgo.Name(
			config.ClientName,
		),

		natsgo.Timeout(
			config.ConnectTimeout,
		),

		natsgo.RetryOnFailedConnect(true),

		natsgo.MaxReconnects(
			unlimitedReconnectAttempts,
		),

		natsgo.ReconnectWait(
			config.ReconnectWait,
		),

		natsgo.ReconnectBufSize(-1),

		natsgo.DrainTimeout(
			config.DrainTimeout,
		),

		natsgo.ConnectHandler(
			func(_ *natsgo.Conn) {
				logger.Info(
					"NATS connection established",
				)
			},
		),

		natsgo.DisconnectErrHandler(
			func(_ *natsgo.Conn, err error) {
				if err == nil {
					logger.Info(
						"NATS connection disconnected",
					)

					return
				}

				logger.Warn(
					"NATS connection disconnected",
					"error",
					err,
				)
			},
		),

		natsgo.ReconnectHandler(
			func(_ *natsgo.Conn) {
				logger.Info(
					"NATS connection re-established",
				)
			},
		),

		natsgo.ReconnectErrHandler(
			func(_ *natsgo.Conn, err error) {
				logger.Warn(
					"NATS reconnect attempt failed",
					"error",
					err,
				)
			},
		),

		natsgo.ErrorHandler(
			func(
				_ *natsgo.Conn,
				_ *natsgo.Subscription,
				err error,
			) {
				logger.Error(
					"NATS asynchronous error",
					"error",
					err,
				)
			},
		),

		natsgo.ClosedHandler(
			func(conn *natsgo.Conn) {
				lastErr := conn.LastError()

				if lastErr != nil {
					logger.Warn(
						"NATS connection closed",
						"error",
						lastErr,
					)

					return
				}

				logger.Info(
					"NATS connection closed",
				)
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open NATS connection: %w",
			err,
		)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()

		return nil, fmt.Errorf(
			"create JetStream client: %w",
			err,
		)
	}

	return &Connection{
		conn:      conn,
		jetStream: js,
	}, nil
}

func (c *Connection) JetStream() jetstream.JetStream {
	if c == nil {
		return nil
	}

	return c.jetStream
}

func (c *Connection) Drain() error {
	if c == nil || c.conn == nil {
		return nil
	}

	if c.conn.IsClosed() {
		return nil
	}

	if err := c.conn.Drain(); err != nil {
		c.conn.Close()

		return fmt.Errorf(
			"drain NATS connection: %w",
			err,
		)
	}

	return nil
}

func validateConnectionConfig(
	config ConnectionConfig,
) error {
	if err := validateConnectionString(
		"NATS URL",
		config.URL,
	); err != nil {
		return err
	}

	if err := validateConnectionString(
		"NATS client name",
		config.ClientName,
	); err != nil {
		return err
	}

	if config.ConnectTimeout <= 0 {
		return errors.New(
			"NATS connect timeout must be positive",
		)
	}

	if config.ReconnectWait <= 0 {
		return errors.New(
			"NATS reconnect wait must be positive",
		)
	}

	if config.DrainTimeout <= 0 {
		return errors.New(
			"NATS drain timeout must be positive",
		)
	}

	return nil
}

func validateConnectionString(
	name string,
	value string,
) error {
	trimmed := strings.TrimSpace(value)

	if trimmed == "" {
		return fmt.Errorf(
			"%s cannot be blank",
			name,
		)
	}

	if value != trimmed {
		return fmt.Errorf(
			"%s must be trimmed",
			name,
		)
	}

	return nil
}
