package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"
)

const valkeyPingTimeout = 5 * time.Second

func NewValkeyClient(
	ctx context.Context,
	address string,
	password string,
) (valkey.Client, error) {
	if address == "" {
		return nil, errors.New(
			"Valkey address cannot be empty",
		)
	}

	if password == "" {
		return nil, errors.New(
			"Valkey password cannot be empty",
		)
	}

	client, err := valkey.NewClient(
		valkey.ClientOption{
			InitAddress: []string{
				address,
			},
			Password: password,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create Valkey client: %w",
			err,
		)
	}

	pingContext, cancel := context.WithTimeout(
		ctx,
		valkeyPingTimeout,
	)
	defer cancel()

	err = client.Do(
		pingContext,
		client.B().
			Ping().
			Build(),
	).Error()
	if err != nil {
		client.Close()

		return nil, fmt.Errorf(
			"ping Valkey: %w",
			err,
		)
	}

	return client, nil
}
