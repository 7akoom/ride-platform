package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type service struct {
	challengeRepository   ChallengeRepository
	identityRepository    IdentityRepository
	otpGenerator          OTPGenerator
	otpHasher             OTPHasher
	otpDelivery           OTPDelivery
	otpRequestRateLimiter OTPRequestRateLimiter

	challengeIDGenerator ChallengeIDGenerator
	tokenIssuer          TokenIssuer

	refreshTokenRotationStore RefreshTokenRotationStore
	refreshTokenGenerator     RefreshTokenGenerator
	refreshTokenHasher        RefreshTokenHasher
	accessTokenSigner         AccessTokenSigner

	clock Clock

	otpTTL                    time.Duration
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy
	refreshTokenTTL           time.Duration
}

var _ Service = (*service)(nil)

func NewService(
	challengeRepository ChallengeRepository,
	identityRepository IdentityRepository,
	otpGenerator OTPGenerator,
	otpHasher OTPHasher,
	otpDelivery OTPDelivery,
	otpRequestRateLimiter OTPRequestRateLimiter,
	challengeIDGenerator ChallengeIDGenerator,
	tokenIssuer TokenIssuer,
	refreshTokenRotationStore RefreshTokenRotationStore,
	refreshTokenGenerator RefreshTokenGenerator,
	refreshTokenHasher RefreshTokenHasher,
	accessTokenSigner AccessTokenSigner,
	clock Clock,
	otpTTL time.Duration,
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy,
	refreshTokenTTL time.Duration,
) Service {
	return &service{
		challengeRepository:       challengeRepository,
		identityRepository:        identityRepository,
		otpGenerator:              otpGenerator,
		otpHasher:                 otpHasher,
		otpDelivery:               otpDelivery,
		otpRequestRateLimiter:     otpRequestRateLimiter,
		challengeIDGenerator:      challengeIDGenerator,
		tokenIssuer:               tokenIssuer,
		refreshTokenRotationStore: refreshTokenRotationStore,
		refreshTokenGenerator:     refreshTokenGenerator,
		refreshTokenHasher:        refreshTokenHasher,
		accessTokenSigner:         accessTokenSigner,
		clock:                     clock,
		otpTTL:                    otpTTL,
		otpRequestRateLimitPolicy: otpRequestRateLimitPolicy,
		refreshTokenTTL:           refreshTokenTTL,
	}
}

func (s *service) RequestOTP(
	ctx context.Context,
	input RequestOTPInput,
) (RequestOTPResult, error) {
	phoneNumber, err := NormalizePhoneNumber(
		input.PhoneNumber,
	)
	if err != nil {
		return RequestOTPResult{}, err
	}

	now := s.clock.Now()

	if err := s.otpRequestRateLimiter.Allow(
		ctx,
		phoneNumber,
		now,
		s.otpRequestRateLimitPolicy,
	); err != nil {
		if errors.Is(
			err,
			ErrOTPRequestRateLimited,
		) {
			return RequestOTPResult{},
				ErrOTPRequestRateLimited
		}

		return RequestOTPResult{}, fmt.Errorf(
			"apply OTP request rate limit: %w",
			err,
		)
	}

	code, err := s.otpGenerator.Generate()
	if err != nil {
		return RequestOTPResult{}, fmt.Errorf(
			"generate OTP: %w",
			err,
		)
	}

	codeHash, err := s.otpHasher.Hash(code)
	if err != nil {
		return RequestOTPResult{}, fmt.Errorf(
			"hash OTP: %w",
			err,
		)
	}

	challengeID, err := s.challengeIDGenerator.Generate()
	if err != nil {
		return RequestOTPResult{}, fmt.Errorf(
			"generate challenge ID: %w",
			err,
		)
	}

	challenge := OTPChallenge{
		ID:          challengeID,
		PhoneNumber: phoneNumber,
		CodeHash:    codeHash,
		ExpiresAt:   now.Add(s.otpTTL),
	}

	if err := s.challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		return RequestOTPResult{}, fmt.Errorf(
			"create OTP challenge: %w",
			err,
		)
	}

	if deliveryErr := s.otpDelivery.Send(
		ctx,
		phoneNumber,
		code,
	); deliveryErr != nil {
		cancelledAt := s.clock.Now()

		if cancelErr := s.challengeRepository.Cancel(
			ctx,
			challenge.ID,
			cancelledAt,
		); cancelErr != nil {
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

		return RequestOTPResult{}, fmt.Errorf(
			"deliver OTP: %w",
			deliveryErr,
		)
	}

	return RequestOTPResult{
		ChallengeID:      challengeID,
		ExpiresInSeconds: int32(s.otpTTL.Seconds()),
	}, nil
}

func (s *service) VerifyOTP(
	ctx context.Context,
	input VerifyOTPInput,
) (VerifyOTPResult, error) {
	challenge, err := s.challengeRepository.FindByID(
		ctx,
		input.ChallengeID,
	)
	if err != nil {
		if errors.Is(err, ErrChallengeNotFound) {
			return VerifyOTPResult{}, ErrChallengeNotFound
		}

		return VerifyOTPResult{}, fmt.Errorf(
			"find OTP challenge: %w",
			err,
		)
	}

	if challenge.VerifiedAt != nil {
		return VerifyOTPResult{}, ErrChallengeUsed
	}

	if challenge.CancelledAt != nil {
		return VerifyOTPResult{}, ErrChallengeCancelled
	}

	now := s.clock.Now()

	if !now.Before(challenge.ExpiresAt) {
		return VerifyOTPResult{}, ErrChallengeExpired
	}

	if challenge.FailedAttempts >= challenge.MaxAttempts {
		return VerifyOTPResult{}, ErrChallengeAttemptsExceeded
	}

	if err := s.otpHasher.Compare(
		challenge.CodeHash,
		input.Code,
	); err != nil {
		recordErr := s.challengeRepository.RecordFailedAttempt(
			ctx,
			challenge.ID,
			now,
		)

		if recordErr != nil {
			switch {
			case errors.Is(
				recordErr,
				ErrChallengeNotFound,
			):
				return VerifyOTPResult{}, ErrChallengeNotFound

			case errors.Is(
				recordErr,
				ErrChallengeExpired,
			):
				return VerifyOTPResult{}, ErrChallengeExpired

			case errors.Is(
				recordErr,
				ErrChallengeUsed,
			):
				return VerifyOTPResult{}, ErrChallengeUsed

			case errors.Is(
				recordErr,
				ErrChallengeAttemptsExceeded,
			):
				return VerifyOTPResult{},
					ErrChallengeAttemptsExceeded

			default:
				return VerifyOTPResult{}, fmt.Errorf(
					"record failed OTP attempt: %w",
					recordErr,
				)
			}
		}

		return VerifyOTPResult{}, ErrInvalidOTP
	}

	identity, err :=
		s.identityRepository.FindOrCreateByPhoneNumber(
			ctx,
			challenge.PhoneNumber,
		)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"find or create identity: %w",
			err,
		)
	}

	if !identity.IsActive {
		return VerifyOTPResult{}, ErrIdentityInactive
	}

	if err := s.challengeRepository.MarkVerified(
		ctx,
		challenge.ID,
		now,
	); err != nil {
		switch {
		case errors.Is(err, ErrChallengeNotFound):
			return VerifyOTPResult{}, ErrChallengeNotFound

		case errors.Is(err, ErrChallengeExpired):
			return VerifyOTPResult{}, ErrChallengeExpired

		case errors.Is(err, ErrChallengeUsed):
			return VerifyOTPResult{}, ErrChallengeUsed

		case errors.Is(
			err,
			ErrChallengeAttemptsExceeded,
		):
			return VerifyOTPResult{},
				ErrChallengeAttemptsExceeded

		default:
			return VerifyOTPResult{}, fmt.Errorf(
				"mark OTP challenge verified: %w",
				err,
			)
		}
	}

	tokenPair, err := s.tokenIssuer.Issue(
		ctx,
		identity,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"issue token pair: %w",
			err,
		)
	}

	return VerifyOTPResult{
		IdentityID: identity.ID,
		AccessToken: tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		AccessTokenExpiresInSeconds: tokenPair.AccessTokenExpiresInSeconds,
	}, nil
}

func (s *service) RefreshToken(
	ctx context.Context,
	input RefreshTokenInput,
) (RefreshTokenResult, error) {
	if input.RefreshToken == "" {
		return RefreshTokenResult{}, ErrInvalidRefreshToken
	}

	currentTokenHash := s.refreshTokenHasher.Hash(
		input.RefreshToken,
	)

	now := s.clock.Now().UTC()

	refreshContext, err :=
		s.refreshTokenRotationStore.Inspect(
			ctx,
			currentTokenHash,
			now,
		)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRefreshToken):
			return RefreshTokenResult{},
				ErrInvalidRefreshToken

		case errors.Is(err, ErrRefreshTokenExpired):
			return RefreshTokenResult{},
				ErrRefreshTokenExpired

		case errors.Is(err, ErrRefreshTokenRevoked):
			return RefreshTokenResult{},
				ErrRefreshTokenRevoked

		case errors.Is(err, ErrRefreshTokenReused):
			return RefreshTokenResult{},
				ErrRefreshTokenReused

		case errors.Is(err, ErrSessionExpired):
			return RefreshTokenResult{},
				ErrSessionExpired

		case errors.Is(err, ErrSessionRevoked):
			return RefreshTokenResult{},
				ErrSessionRevoked

		case errors.Is(err, ErrIdentityInactive):
			return RefreshTokenResult{},
				ErrIdentityInactive

		default:
			return RefreshTokenResult{}, fmt.Errorf(
				"inspect refresh token: %w",
				err,
			)
		}
	}

	replacementRefreshToken, err :=
		s.refreshTokenGenerator.Generate()
	if err != nil {
		return RefreshTokenResult{}, fmt.Errorf(
			"generate replacement refresh token: %w",
			err,
		)
	}

	replacementTokenHash := s.refreshTokenHasher.Hash(
		replacementRefreshToken,
	)

	replacementExpiresAt := now.Add(
		s.refreshTokenTTL,
	)

	if replacementExpiresAt.After(
		refreshContext.SessionExpiresAt,
	) {
		replacementExpiresAt =
			refreshContext.SessionExpiresAt
	}

	if !replacementExpiresAt.After(now) {
		return RefreshTokenResult{},
			ErrSessionExpired
	}

	accessToken, accessTokenExpiresInSeconds, err :=
		s.accessTokenSigner.Issue(
			refreshContext.IdentityID,
			refreshContext.SessionID,
			now,
		)
	if err != nil {
		return RefreshTokenResult{}, fmt.Errorf(
			"issue refreshed access token: %w",
			err,
		)
	}

	err = s.refreshTokenRotationStore.Rotate(
		ctx,
		RefreshTokenRotationInput{
			CurrentTokenHash:     currentTokenHash,
			ReplacementTokenHash: replacementTokenHash,
			RotatedAt:            now,
			ReplacementExpiresAt: replacementExpiresAt,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRefreshToken):
			return RefreshTokenResult{},
				ErrInvalidRefreshToken

		case errors.Is(err, ErrRefreshTokenExpired):
			return RefreshTokenResult{},
				ErrRefreshTokenExpired

		case errors.Is(err, ErrRefreshTokenRevoked):
			return RefreshTokenResult{},
				ErrRefreshTokenRevoked

		case errors.Is(err, ErrRefreshTokenReused):
			return RefreshTokenResult{},
				ErrRefreshTokenReused

		case errors.Is(err, ErrSessionExpired):
			return RefreshTokenResult{},
				ErrSessionExpired

		case errors.Is(err, ErrSessionRevoked):
			return RefreshTokenResult{},
				ErrSessionRevoked

		case errors.Is(err, ErrIdentityInactive):
			return RefreshTokenResult{},
				ErrIdentityInactive

		default:
			return RefreshTokenResult{}, fmt.Errorf(
				"rotate refresh token: %w",
				err,
			)
		}
	}

	return RefreshTokenResult{
		IdentityID: refreshContext.IdentityID,
		AccessToken: accessToken,
		RefreshToken: replacementRefreshToken,
		AccessTokenExpiresInSeconds: accessTokenExpiresInSeconds,
	}, nil
}