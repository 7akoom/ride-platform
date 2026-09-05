package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *service) RequestOTP(
	ctx context.Context,
	input RequestOTPInput,
) (RequestOTPResult, error) {
	identifier, purpose, targetIdentityID, channel, err :=
		normalizeRequestOTPInput(input)
	if err != nil {
		return RequestOTPResult{}, err
	}

	recordRequestOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordOTPRequest(
			ctx,
			purpose,
			channel,
			outcome,
		)
	}

	recordSecurityEvent := func(
		event SecurityMetricEvent,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordSecurityEvent(
			ctx,
			event,
		)
	}

	tenantHint, err := NormalizeTenantHint(
		input.TenantHint,
	)
	if err != nil {
		recordRequestOutcome(
			MetricOutcomeRejected,
		)

		return RequestOTPResult{}, err
	}

	now := s.clock.Now()

	if err := s.otpRequestRateLimiter.Allow(
		ctx,
		OTPRequestScope{
			Identifier:       identifier,
			Purpose:          purpose,
			TargetIdentityID: targetIdentityID,
			SourceIPAddress: strings.TrimSpace(
				input.SourceIPAddress,
			),
		},
		now,
		s.otpRequestRateLimitPolicy,
	); err != nil {
		if errors.Is(
			err,
			ErrOTPRequestRateLimited,
		) {
			recordRequestOutcome(
				MetricOutcomeRejected,
			)

			recordSecurityEvent(
				SecurityMetricEventOTPRateLimited,
			)

			return RequestOTPResult{},
				ErrOTPRequestRateLimited
		}

		recordRequestOutcome(
			MetricOutcomeFailed,
		)

		return RequestOTPResult{}, fmt.Errorf(
			"apply OTP request rate limit: %w",
			err,
		)
	}

	code, err := s.otpGenerator.Generate()
	if err != nil {
		recordRequestOutcome(
			MetricOutcomeFailed,
		)

		return RequestOTPResult{}, fmt.Errorf(
			"generate OTP: %w",
			err,
		)
	}

	challengeID, err := s.challengeIDGenerator.Generate()
	if err != nil {
		recordRequestOutcome(
			MetricOutcomeFailed,
		)

		return RequestOTPResult{}, fmt.Errorf(
			"generate challenge ID: %w",
			err,
		)
	}

	codeHash, err := s.otpHasher.Hash(
		challengeID,
		code,
	)
	if err != nil {
		recordRequestOutcome(
			MetricOutcomeFailed,
		)

		return RequestOTPResult{}, fmt.Errorf(
			"hash OTP: %w",
			err,
		)
	}

	challenge := OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          purpose,
		TargetIdentityID: targetIdentityID,
		TenantHint:       tenantHint,
		CodeHash:         codeHash,
		ExpiresAt:        now.Add(s.otpTTL),
	}

	if err := s.challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		recordRequestOutcome(
			MetricOutcomeFailed,
		)

		return RequestOTPResult{}, fmt.Errorf(
			"create OTP challenge: %w",
			err,
		)
	}

	if deliveryErr := s.otpDelivery.Send(
		ctx,
		OTPDeliveryInput{
			ChallengeID: challengeID,
			Identifier:  identifier,
			Code:        code,
			Purpose:     purpose,
			Channel:     channel,
			Locale:      input.Locale,
		},
	); deliveryErr != nil {
		cancelledAt := s.clock.Now()

		compensationCtx, cancelCompensation :=
			context.WithTimeout(
				context.WithoutCancel(ctx),
				5*time.Second,
			)
		defer cancelCompensation()

		if cancelErr := s.challengeRepository.Cancel(
			compensationCtx,
			challenge.ID,
			cancelledAt,
		); cancelErr != nil {
			recordRequestOutcome(
				MetricOutcomeFailed,
			)

			return RequestOTPResult{}, errors.Join(
				fmt.Errorf(
					"deliver OTP: %w",
					deliveryErr,
				),
				fmt.Errorf(
					"cancel OTP challenge after delivery failure: %w",
					cancelErr,
				),
			)
		}

		recordRequestOutcome(
			MetricOutcomeFailed,
		)

		return RequestOTPResult{}, fmt.Errorf(
			"deliver OTP: %w",
			deliveryErr,
		)
	}

	recordRequestOutcome(
		MetricOutcomeSuccess,
	)

	return RequestOTPResult{
		ChallengeID:      challengeID,
		ExpiresInSeconds: int32(s.otpTTL.Seconds()),
	}, nil
}

func normalizeRequestOTPInput(
	input RequestOTPInput,
) (
	Identifier,
	OTPPurpose,
	*string,
	OTPDeliveryChannel,
	error,
) {
	identifier, err := NewIdentifier(
		input.Identifier.Type,
		input.Identifier.Value,
	)
	if err != nil {
		return Identifier{}, "", nil, "", err
	}

	purpose := input.Purpose

	parsedPurpose, err := ParseOTPPurpose(
		string(purpose),
	)
	if err != nil {
		return Identifier{}, "", nil, "", err
	}

	channel, err := ParseOTPDeliveryChannel(
		string(input.Channel),
	)
	if err != nil {
		return Identifier{}, "", nil, "", err
	}

	switch channel {
	case OTPDeliveryChannelAuto:

	case OTPDeliveryChannelSMS,
		OTPDeliveryChannelWhatsApp:
		if identifier.Type != IdentifierTypePhone {
			return Identifier{},
				"",
				nil,
				"",
				fmt.Errorf(
					"%w: %s requires a phone identifier",
					ErrInvalidOTPDeliveryChannel,
					channel,
				)
		}

	case OTPDeliveryChannelEmail:
		if identifier.Type != IdentifierTypeEmail {
			return Identifier{},
				"",
				nil,
				"",
				fmt.Errorf(
					"%w: email requires an email identifier",
					ErrInvalidOTPDeliveryChannel,
				)
		}

	default:
		return Identifier{},
			"",
			nil,
			"",
			ErrInvalidOTPDeliveryChannel
	}

	targetIdentityID := input.TargetIdentityID

	switch parsedPurpose {
	case OTPPurposeLogin:
		if targetIdentityID != nil {
			return Identifier{},
				"",
				nil,
				"",
				errors.New(
					"login OTP request cannot target an identity",
				)
		}

	case OTPPurposeLinkIdentifier:
		if targetIdentityID == nil {
			return Identifier{},
				"",
				nil,
				"",
				errors.New(
					"link identifier OTP request requires target identity",
				)
		}

		normalizedTargetIdentityID :=
			strings.TrimSpace(*targetIdentityID)

		if normalizedTargetIdentityID == "" {
			return Identifier{},
				"",
				nil,
				"",
				errors.New(
					"OTP request target identity cannot be blank",
				)
		}

		targetIdentityID =
			&normalizedTargetIdentityID

	default:
		return Identifier{},
			"",
			nil,
			"",
			ErrInvalidOTPPurpose
	}

	return identifier,
		parsedPurpose,
		targetIdentityID,
		channel,
		nil
}
