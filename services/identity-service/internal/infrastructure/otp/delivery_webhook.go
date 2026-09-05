package otp

import "errors"

var (
	ErrDeliveryWebhookUnauthorized = errors.New(
		"delivery webhook unauthorized",
	)

	ErrDeliveryWebhookIgnored = errors.New(
		"delivery webhook ignored",
	)

	ErrDeliveryWebhookInvalid = errors.New(
		"delivery webhook invalid",
	)
)
