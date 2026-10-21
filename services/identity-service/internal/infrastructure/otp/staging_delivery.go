package otp

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

// StagingSimulatedPhonePrefix is the range the dev scripts and the deploy smoke
// test use for their test people (+9647700000991 the staff, +9647700000992 the
// rider, …): nobody has WhatsApp there, so their codes go to the log.
const StagingSimulatedPhonePrefix = "+96477000009"

// StagingDelivery sends codes the real way where it can: phone codes by
// WhatsApp, email codes by email, each when it is given a sender. So a staging
// instance can be tried on a real phone and inbox the way a customer would get
// them. A code that was sent is not written to the log; one that could not be
// (no sender, a test number, the provider failed) is, through the development
// delivery, so a sign-in never gets stuck.
type StagingDelivery struct {
	log      auth.OTPDelivery
	whatsApp WhatsAppSender
	email    EmailSender
	logger   *slog.Logger
}

// NewStagingDelivery needs at least one of whatsApp and email; a nil one keeps
// that channel in the log.
func NewStagingDelivery(
	environment string,
	log auth.OTPDelivery,
	whatsApp WhatsAppSender,
	email EmailSender,
	logger *slog.Logger,
) (*StagingDelivery, error) {
	switch {
	case environment != "development" && environment != "test":
		return nil, errors.New("staging OTP delivery is only allowed in development or test environments")
	case log == nil:
		return nil, errors.New("staging OTP delivery needs the development delivery")
	case whatsApp == nil && email == nil:
		return nil, errors.New("staging OTP delivery needs a WhatsApp or an email sender")
	case logger == nil:
		return nil, errors.New("staging OTP delivery logger is required")
	}

	return &StagingDelivery{log: log, whatsApp: whatsApp, email: email, logger: logger}, nil
}

func (d *StagingDelivery) Send(
	ctx context.Context,
	input auth.OTPDeliveryInput,
) error {
	var (
		channel string
		err     error
	)

	switch {
	case input.Identifier.Type == auth.IdentifierTypeEmail && d.email != nil:
		channel = "email"
		err = d.email.Send(ctx, strings.TrimSpace(input.Identifier.Value), input.Code, input.Purpose, input.Locale)

	case input.Identifier.Type == auth.IdentifierTypePhone && d.whatsApp != nil &&
		input.Channel != auth.OTPDeliveryChannelEmail &&
		!strings.HasPrefix(strings.TrimSpace(input.Identifier.Value), StagingSimulatedPhonePrefix):
		phone := strings.TrimSpace(input.Identifier.Value)
		channel = "WhatsApp"

		if sender, ok := d.whatsApp.(ChallengeAwareWhatsAppSender); ok &&
			strings.TrimSpace(input.ChallengeID) != "" {
			err = sender.SendForChallenge(ctx, input.ChallengeID, phone, input.Code, input.Purpose, input.Locale)
		} else {
			err = d.whatsApp.Send(ctx, phone, input.Code, input.Purpose, input.Locale)
		}

	default:
		return d.log.Send(ctx, input)
	}

	if err != nil {
		d.logger.WarnContext(ctx, "staging: the code was not sent by "+channel+"; it is written to the log instead",
			"otp_challenge_id", input.ChallengeID,
			"error", err,
		)

		return d.log.Send(ctx, input)
	}

	d.logger.InfoContext(ctx, "staging: the code was sent by "+channel,
		"otp_challenge_id", input.ChallengeID,
	)

	return nil
}
