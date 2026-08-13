package otp

import (
	"context"
	"errors"
	"log/slog"
)

type DevelopmentDelivery struct {
	logger *slog.Logger
}

func NewDevelopmentDelivery(
	environment string,
	logger *slog.Logger,
) (*DevelopmentDelivery, error) {
	if environment != "development" && environment != "test" {
		return nil, errors.New(
			"development OTP delivery is only allowed in development or test environments",
		)
	}

	if logger == nil {
		return nil, errors.New(
			"development OTP delivery logger is required",
		)
	}

	return &DevelopmentDelivery{
		logger: logger,
	}, nil
}

func (d *DevelopmentDelivery) Send(
	ctx context.Context,
	identifier string,
	code string,
) error {
	d.logger.WarnContext(
		ctx,
		"OTP delivered through development adapter",
		"otp_identifier", identifier,
		"otp_code", code,
	)

	return nil
}
