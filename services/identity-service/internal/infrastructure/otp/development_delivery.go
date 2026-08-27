package otp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
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
	input auth.OTPDeliveryInput,
) error {
	d.logger.WarnContext(
		ctx,
		"OTP delivered through development adapter",
		"otp_identifier_type", input.Identifier.Type,
		"otp_identifier", input.Identifier.Value,
		"otp_purpose", input.Purpose,
		"otp_code", input.Code,
	)

	return nil
}
