package otp

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

// StagingSimulatedPhonePrefix is the range the dev scripts use for their test
// people (+9647700000991 the staff, +9647700000992 the rider, …): nobody has
// WhatsApp there, so their codes stay in the log only.
const StagingSimulatedPhonePrefix = "+96477000009"

// StagingDelivery is the development delivery (the code in the log) that also
// sends phone codes by WhatsApp, so a staging instance can be tried on a real
// phone the way a customer would get it. A WhatsApp failure is logged and never
// stops the sign-in: the code is in the log either way.
type StagingDelivery struct {
	log      auth.OTPDelivery
	whatsApp WhatsAppSender
	logger   *slog.Logger
}

func NewStagingDelivery(
	environment string,
	log auth.OTPDelivery,
	whatsApp WhatsAppSender,
	logger *slog.Logger,
) (*StagingDelivery, error) {
	switch {
	case environment != "development" && environment != "test":
		return nil, errors.New("staging OTP delivery is only allowed in development or test environments")
	case log == nil:
		return nil, errors.New("staging OTP delivery needs the development delivery")
	case whatsApp == nil:
		return nil, errors.New("staging OTP delivery needs a WhatsApp sender (WHATSAPP_DEFAULT_PROVIDER)")
	case logger == nil:
		return nil, errors.New("staging OTP delivery logger is required")
	}

	return &StagingDelivery{log: log, whatsApp: whatsApp, logger: logger}, nil
}

func (d *StagingDelivery) Send(
	ctx context.Context,
	input auth.OTPDeliveryInput,
) error {
	if err := d.log.Send(ctx, input); err != nil {
		return err
	}

	if input.Identifier.Type != auth.IdentifierTypePhone ||
		input.Channel == auth.OTPDeliveryChannelEmail {
		return nil
	}

	phone := strings.TrimSpace(input.Identifier.Value)
	if strings.HasPrefix(phone, StagingSimulatedPhonePrefix) {
		return nil
	}

	var err error

	if sender, ok := d.whatsApp.(ChallengeAwareWhatsAppSender); ok &&
		strings.TrimSpace(input.ChallengeID) != "" {
		err = sender.SendForChallenge(ctx, input.ChallengeID, phone, input.Code, input.Purpose, input.Locale)
	} else {
		err = d.whatsApp.Send(ctx, phone, input.Code, input.Purpose, input.Locale)
	}

	if err != nil {
		d.logger.WarnContext(ctx, "staging: the WhatsApp code was not sent (it is in the log above)",
			"otp_challenge_id", input.ChallengeID,
			"error", err,
		)

		return nil
	}

	d.logger.InfoContext(ctx, "staging: the code was sent by WhatsApp too",
		"otp_challenge_id", input.ChallengeID,
	)

	return nil
}
