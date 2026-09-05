package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *service) completeIdentifierUnlink(
	ctx context.Context,
	challenge OTPChallenge,
	verifiedAt time.Time,
) (VerifyOTPResult, error) {
	if s.identifierUnlinkCompletionStore == nil {
		return VerifyOTPResult{}, errors.New(
			"identifier unlink completion store is not configured",
		)
	}

	if challenge.TargetIdentityID == nil {
		return VerifyOTPResult{}, errors.New(
			"unlink identifier OTP challenge has no target identity",
		)
	}

	identityID := strings.TrimSpace(
		*challenge.TargetIdentityID,
	)
	if identityID == "" {
		return VerifyOTPResult{}, errors.New(
			"unlink identifier OTP challenge target identity is blank",
		)
	}

	err := s.identifierUnlinkCompletionStore.Complete(
		ctx,
		IdentifierUnlinkCompletionInput{
			ChallengeID: challenge.ID,
			IdentityID:  identityID,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrChallengeNotFound,
		):
			return VerifyOTPResult{},
				ErrChallengeNotFound

		case errors.Is(
			err,
			ErrChallengeExpired,
		):
			return VerifyOTPResult{},
				ErrChallengeExpired

		case errors.Is(
			err,
			ErrChallengeUsed,
		):
			return VerifyOTPResult{},
				ErrChallengeUsed

		case errors.Is(
			err,
			ErrChallengeCancelled,
		):
			return VerifyOTPResult{},
				ErrChallengeCancelled

		case errors.Is(
			err,
			ErrChallengeAttemptsExceeded,
		):
			return VerifyOTPResult{},
				ErrChallengeAttemptsExceeded

		case errors.Is(
			err,
			ErrIdentityNotFound,
		):
			return VerifyOTPResult{},
				ErrIdentityNotFound

		case errors.Is(
			err,
			ErrIdentifierNotLinked,
		):
			return VerifyOTPResult{},
				ErrIdentifierNotLinked

		case errors.Is(
			err,
			ErrLastIdentifierRemoval,
		):
			return VerifyOTPResult{},
				ErrLastIdentifierRemoval

		default:
			return VerifyOTPResult{}, fmt.Errorf(
				"complete identifier unlink: %w",
				err,
			)
		}
	}

	return VerifyOTPResult{
		IdentityID: identityID,
	}, nil
}

func (s *service) RequestIdentifierUnlinkOTP(
	ctx context.Context,
	input RequestIdentifierUnlinkOTPInput,
) (RequestIdentifierUnlinkOTPResult, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return RequestIdentifierUnlinkOTPResult{},
			ErrIdentityNotFound
	}

	targetIdentifier, err := NewIdentifier(
		input.TargetIdentifier.Type,
		input.TargetIdentifier.Value,
	)
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{}, err
	}

	channel, err := ParseOTPDeliveryChannel(
		string(input.Channel),
	)
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{}, err
	}

	details, found, err := s.identityReader.FindByID(
		ctx,
		identityID,
	)
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{},
			fmt.Errorf(
				"get identity details for identifier unlink: %w",
				err,
			)
	}

	if !found {
		return RequestIdentifierUnlinkOTPResult{},
			ErrIdentityNotFound
	}

	targetLinked := false

	var verificationIdentifier Identifier
	verificationIdentifierFound := false

	for _, identityIdentifier := range details.Identifiers {
		identifier, err := NewIdentifier(
			identityIdentifier.Identifier.Type,
			identityIdentifier.Identifier.Value,
		)
		if err != nil {
			return RequestIdentifierUnlinkOTPResult{},
				fmt.Errorf(
					"validate identity identifier for unlink: %w",
					err,
				)
		}

		if identifier.Type == targetIdentifier.Type &&
			identifier.Value == targetIdentifier.Value {
			targetLinked = true
			continue
		}

		if !verificationIdentifierFound {
			switch channel {
			case OTPDeliveryChannelAuto:
				verificationIdentifier = identifier
				verificationIdentifierFound = true

			case OTPDeliveryChannelSMS,
				OTPDeliveryChannelWhatsApp:
				if identifier.Type == IdentifierTypePhone {
					verificationIdentifier = identifier
					verificationIdentifierFound = true
				}

			case OTPDeliveryChannelEmail:
				if identifier.Type == IdentifierTypeEmail {
					verificationIdentifier = identifier
					verificationIdentifierFound = true
				}
			}
		}
	}

	if !targetLinked {
		return RequestIdentifierUnlinkOTPResult{},
			ErrIdentifierNotLinked
	}

	if len(details.Identifiers) <= 1 {
		return RequestIdentifierUnlinkOTPResult{},
			ErrLastIdentifierRemoval
	}

	if !verificationIdentifierFound {
		return RequestIdentifierUnlinkOTPResult{},
			ErrOTPDeliveryChannelUnavailable
	}

	tenantHint, err := NormalizeTenantHint(
		input.TenantHint,
	)
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{}, err
	}

	now := s.clock.Now()

	targetIdentityID := identityID

	if err := s.otpRequestRateLimiter.Allow(
		ctx,
		OTPRequestScope{
			Identifier:       verificationIdentifier,
			Purpose:          OTPPurposeUnlinkIdentifier,
			TargetIdentityID: &targetIdentityID,
			SourceIPAddress:  strings.TrimSpace(input.SourceIPAddress),
		},
		now,
		s.otpRequestRateLimitPolicy,
	); err != nil {
		if errors.Is(
			err,
			ErrOTPRequestRateLimited,
		) {
			return RequestIdentifierUnlinkOTPResult{},
				ErrOTPRequestRateLimited
		}

		return RequestIdentifierUnlinkOTPResult{},
			fmt.Errorf(
				"apply identifier unlink OTP request rate limit: %w",
				err,
			)
	}

	code, err := s.otpGenerator.Generate()
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{},
			fmt.Errorf(
				"generate identifier unlink OTP: %w",
				err,
			)
	}

	challengeID, err := s.challengeIDGenerator.Generate()
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{},
			fmt.Errorf(
				"generate identifier unlink challenge ID: %w",
				err,
			)
	}

	codeHash, err := s.otpHasher.Hash(
		challengeID,
		code,
	)
	if err != nil {
		return RequestIdentifierUnlinkOTPResult{},
			fmt.Errorf(
				"hash identifier unlink OTP: %w",
				err,
			)
	}

	challenge := OTPChallenge{
		ID:               challengeID,
		Identifier:       verificationIdentifier,
		Purpose:          OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &targetIdentityID,
		TenantHint:       tenantHint,
		CodeHash:         codeHash,
		ExpiresAt:        now.Add(s.otpTTL),
	}

	if err := s.identifierUnlinkRequestStore.Create(
		ctx,
		IdentifierUnlinkRequestInput{
			Challenge:        challenge,
			TargetIdentifier: targetIdentifier,
		},
	); err != nil {
		switch {
		case errors.Is(
			err,
			ErrIdentityNotFound,
		):
			return RequestIdentifierUnlinkOTPResult{},
				ErrIdentityNotFound

		case errors.Is(
			err,
			ErrIdentifierNotLinked,
		):
			return RequestIdentifierUnlinkOTPResult{},
				ErrIdentifierNotLinked

		case errors.Is(
			err,
			ErrLastIdentifierRemoval,
		):
			return RequestIdentifierUnlinkOTPResult{},
				ErrLastIdentifierRemoval

		default:
			return RequestIdentifierUnlinkOTPResult{},
				fmt.Errorf(
					"create identifier unlink OTP request: %w",
					err,
				)
		}
	}

	if deliveryErr := s.otpDelivery.Send(
		ctx,
		OTPDeliveryInput{
			ChallengeID: challenge.ID,
			Identifier:  verificationIdentifier,
			Code:        code,
			Purpose:     OTPPurposeUnlinkIdentifier,
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
			return RequestIdentifierUnlinkOTPResult{},
				errors.Join(
					fmt.Errorf(
						"deliver identifier unlink OTP: %w",
						deliveryErr,
					),
					fmt.Errorf(
						"cancel identifier unlink OTP challenge after delivery failure: %w",
						cancelErr,
					),
				)
		}

		return RequestIdentifierUnlinkOTPResult{},
			fmt.Errorf(
				"deliver identifier unlink OTP: %w",
				deliveryErr,
			)
	}

	return RequestIdentifierUnlinkOTPResult{
		ChallengeID:      challengeID,
		ExpiresInSeconds: int32(s.otpTTL.Seconds()),
	}, nil
}
