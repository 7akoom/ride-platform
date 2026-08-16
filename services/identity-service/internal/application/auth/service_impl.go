package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type service struct {
	challengeRepository           ChallengeRepository
	identityIdentifierRepository  IdentityIdentifierRepository
	identityReader                IdentityReader
	identifierLinkCompletionStore IdentifierLinkCompletionStore
	otpGenerator                  OTPGenerator
	otpHasher                     OTPHasher
	otpDelivery                   OTPDelivery
	otpRequestRateLimiter         OTPRequestRateLimiter
	challengeIDGenerator          ChallengeIDGenerator
	tokenIssuer                   TokenIssuer
	refreshTokenRotationStore     RefreshTokenRotationStore
	sessionRevocationStore        SessionRevocationStore
	allSessionsRevocationStore    AllSessionsRevocationStore
	refreshTokenGenerator         RefreshTokenGenerator
	refreshTokenHasher            RefreshTokenHasher
	accessTokenSigner             AccessTokenSigner
	clock                         Clock
	otpTTL                        time.Duration
	otpRequestRateLimitPolicy     OTPRequestRateLimitPolicy
	refreshTokenTTL               time.Duration
}

var _ Service = (*service)(nil)

func NewServiceWithIdentityIdentifiers(
	challengeRepository ChallengeRepository,
	identityIdentifierRepository IdentityIdentifierRepository,
	identityReader IdentityReader,
	identifierLinkCompletionStore IdentifierLinkCompletionStore,
	otpGenerator OTPGenerator,
	otpHasher OTPHasher,
	otpDelivery OTPDelivery,
	otpRequestRateLimiter OTPRequestRateLimiter,
	challengeIDGenerator ChallengeIDGenerator,
	tokenIssuer TokenIssuer,
	refreshTokenRotationStore RefreshTokenRotationStore,
	sessionRevocationStore SessionRevocationStore,
	allSessionsRevocationStore AllSessionsRevocationStore,
	refreshTokenGenerator RefreshTokenGenerator,
	refreshTokenHasher RefreshTokenHasher,
	accessTokenSigner AccessTokenSigner,
	clock Clock,
	otpTTL time.Duration,
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy,
	refreshTokenTTL time.Duration,
) Service {
	if challengeRepository == nil {
		panic("challenge repository is required")
	}

	if identityIdentifierRepository == nil {
		panic("identity identifier repository is required")
	}

	if identityReader == nil {
		panic("identity reader is required")
	}

	if identifierLinkCompletionStore == nil {
		panic("identifier link completion store is required")
	}

	return newServiceWithDependencies(
		challengeRepository,
		identityIdentifierRepository,
		identityReader,
		identifierLinkCompletionStore,
		otpGenerator,
		otpHasher,
		otpDelivery,
		otpRequestRateLimiter,
		challengeIDGenerator,
		tokenIssuer,
		refreshTokenRotationStore,
		sessionRevocationStore,
		allSessionsRevocationStore,
		refreshTokenGenerator,
		refreshTokenHasher,
		accessTokenSigner,
		clock,
		otpTTL,
		otpRequestRateLimitPolicy,
		refreshTokenTTL,
	)
}

func newServiceWithDependencies(
	challengeRepository ChallengeRepository,
	identityIdentifierRepository IdentityIdentifierRepository,
	identityReader IdentityReader,
	identifierLinkCompletionStore IdentifierLinkCompletionStore,
	otpGenerator OTPGenerator,
	otpHasher OTPHasher,
	otpDelivery OTPDelivery,
	otpRequestRateLimiter OTPRequestRateLimiter,
	challengeIDGenerator ChallengeIDGenerator,
	tokenIssuer TokenIssuer,
	refreshTokenRotationStore RefreshTokenRotationStore,
	sessionRevocationStore SessionRevocationStore,
	allSessionsRevocationStore AllSessionsRevocationStore,
	refreshTokenGenerator RefreshTokenGenerator,
	refreshTokenHasher RefreshTokenHasher,
	accessTokenSigner AccessTokenSigner,
	clock Clock,
	otpTTL time.Duration,
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy,
	refreshTokenTTL time.Duration,
) Service {
	if otpGenerator == nil {
		panic("OTP generator is required")
	}

	if otpHasher == nil {
		panic("OTP hasher is required")
	}

	if otpDelivery == nil {
		panic("OTP delivery is required")
	}

	if otpRequestRateLimiter == nil {
		panic("OTP request rate limiter is required")
	}

	if challengeIDGenerator == nil {
		panic("challenge ID generator is required")
	}

	if tokenIssuer == nil {
		panic("token issuer is required")
	}

	if refreshTokenRotationStore == nil {
		panic("refresh token rotation store is required")
	}

	if sessionRevocationStore == nil {
		panic("session revocation store is required")
	}

	if allSessionsRevocationStore == nil {
		panic("all sessions revocation store is required")
	}

	if refreshTokenGenerator == nil {
		panic("refresh token generator is required")
	}

	if refreshTokenHasher == nil {
		panic("refresh token hasher is required")
	}

	if accessTokenSigner == nil {
		panic("access token signer is required")
	}

	if clock == nil {
		panic("clock is required")
	}

	if otpTTL <= 0 {
		panic("OTP TTL must be positive")
	}

	if otpRequestRateLimitPolicy.Cooldown <= 0 {
		panic("OTP request cooldown must be positive")
	}

	if otpRequestRateLimitPolicy.Window <= 0 {
		panic("OTP request window must be positive")
	}

	if otpRequestRateLimitPolicy.MaxRequests <= 0 {
		panic("OTP request max requests must be positive")
	}

	if otpRequestRateLimitPolicy.Cooldown >
		otpRequestRateLimitPolicy.Window {
		panic("OTP request cooldown cannot exceed window")
	}

	if refreshTokenTTL <= 0 {
		panic("refresh token TTL must be positive")
	}

	return &service{
		challengeRepository:           challengeRepository,
		identityIdentifierRepository:  identityIdentifierRepository,
		identityReader:                identityReader,
		identifierLinkCompletionStore: identifierLinkCompletionStore,
		otpGenerator:                  otpGenerator,
		otpHasher:                     otpHasher,
		otpDelivery:                   otpDelivery,
		otpRequestRateLimiter:         otpRequestRateLimiter,
		challengeIDGenerator:          challengeIDGenerator,
		tokenIssuer:                   tokenIssuer,
		refreshTokenRotationStore:     refreshTokenRotationStore,
		sessionRevocationStore:        sessionRevocationStore,
		allSessionsRevocationStore:    allSessionsRevocationStore,
		refreshTokenGenerator:         refreshTokenGenerator,
		refreshTokenHasher:            refreshTokenHasher,
		accessTokenSigner:             accessTokenSigner,
		clock:                         clock,
		otpTTL:                        otpTTL,
		otpRequestRateLimitPolicy:     otpRequestRateLimitPolicy,
		refreshTokenTTL:               refreshTokenTTL,
	}
}

func (s *service) RequestOTP(
	ctx context.Context,
	input RequestOTPInput,
) (RequestOTPResult, error) {
	identifier, purpose, targetIdentityID, err :=
		normalizeRequestOTPInput(input)
	if err != nil {
		return RequestOTPResult{}, err
	}

	now := s.clock.Now()

	if err := s.otpRequestRateLimiter.Allow(
		ctx,
		OTPRequestScope{
			Identifier:       identifier,
			Purpose:          purpose,
			TargetIdentityID: targetIdentityID,
		},
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

	challengeID, err := s.challengeIDGenerator.Generate()
	if err != nil {
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
		CodeHash:         codeHash,
		ExpiresAt:        now.Add(s.otpTTL),
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
		identifier.Value,
		code,
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

func normalizeRequestOTPInput(
	input RequestOTPInput,
) (
	Identifier,
	OTPPurpose,
	*string,
	error,
) {
	identifier, err := NewIdentifier(
		input.Identifier.Type,
		input.Identifier.Value,
	)
	if err != nil {
		return Identifier{}, "", nil, err
	}

	purpose := input.Purpose

	parsedPurpose, err := ParseOTPPurpose(
		string(purpose),
	)
	if err != nil {
		return Identifier{}, "", nil, err
	}

	targetIdentityID := input.TargetIdentityID

	switch parsedPurpose {
	case OTPPurposeLogin:
		if targetIdentityID != nil {
			return Identifier{},
				"",
				nil,
				errors.New(
					"login OTP request cannot target an identity",
				)
		}

	case OTPPurposeLinkIdentifier:
		if targetIdentityID == nil {
			return Identifier{},
				"",
				nil,
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
			ErrInvalidOTPPurpose
	}

	return identifier,
		parsedPurpose,
		targetIdentityID,
		nil
}

func (s *service) VerifyOTP(
	ctx context.Context,
	input VerifyOTPInput,
) (VerifyOTPResult, error) {
	expectedPurpose, err := ParseOTPPurpose(
		string(input.ExpectedPurpose),
	)
	if err != nil {
		return VerifyOTPResult{}, err
	}
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

	if challenge.Purpose != expectedPurpose {
		return VerifyOTPResult{}, ErrOTPPurposeMismatch
	}

	if expectedPurpose == OTPPurposeLinkIdentifier {
		if input.ExpectedTargetIdentityID == nil ||
			challenge.TargetIdentityID == nil {
			return VerifyOTPResult{},
				ErrOTPChallengeTargetMismatch
		}

		expectedTargetIdentityID := strings.TrimSpace(
			*input.ExpectedTargetIdentityID,
		)

		challengeTargetIdentityID := strings.TrimSpace(
			*challenge.TargetIdentityID,
		)

		if expectedTargetIdentityID == "" ||
			challengeTargetIdentityID == "" ||
			expectedTargetIdentityID != challengeTargetIdentityID {
			return VerifyOTPResult{},
				ErrOTPChallengeTargetMismatch
		}
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
		return VerifyOTPResult{},
			ErrChallengeAttemptsExceeded
	}

	otpMatches, err := s.otpHasher.Compare(
		challenge.CodeHash,
		challenge.ID,
		input.Code,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"compare OTP: %w",
			err,
		)
	}

	if !otpMatches {
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
				return VerifyOTPResult{},
					ErrChallengeNotFound

			case errors.Is(
				recordErr,
				ErrChallengeExpired,
			):
				return VerifyOTPResult{},
					ErrChallengeExpired

			case errors.Is(
				recordErr,
				ErrChallengeUsed,
			):
				return VerifyOTPResult{},
					ErrChallengeUsed

			case errors.Is(
				recordErr,
				ErrChallengeCancelled,
			):
				return VerifyOTPResult{},
					ErrChallengeCancelled

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

	switch challenge.Purpose {
	case OTPPurposeLogin:
		return s.completeIdentifierLogin(
			ctx,
			challenge,
			now,
		)

	case OTPPurposeLinkIdentifier:
		return s.completeIdentifierLink(
			ctx,
			challenge,
			now,
		)

	default:
		return VerifyOTPResult{},
			ErrInvalidOTPPurpose
	}
}

func (s *service) completeIdentifierLogin(
	ctx context.Context,
	challenge OTPChallenge,
	verifiedAt time.Time,
) (VerifyOTPResult, error) {
	if s.identityIdentifierRepository == nil {
		return VerifyOTPResult{}, errors.New(
			"identity identifier repository is not configured",
		)
	}

	if challenge.TargetIdentityID != nil {
		return VerifyOTPResult{}, errors.New(
			"login OTP challenge cannot target an identity",
		)
	}

	identifier, err := NewIdentifier(
		challenge.Identifier.Type,
		challenge.Identifier.Value,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"validate login OTP identifier: %w",
			err,
		)
	}

	identity, found, err :=
		s.identityIdentifierRepository.FindIdentityByIdentifier(
			ctx,
			identifier,
		)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"find identity by identifier: %w",
			err,
		)
	}

	if !found {
		identity, err =
			s.identityIdentifierRepository.
				CreateIdentityWithIdentifier(
					ctx,
					identifier,
					verifiedAt,
				)
		if err != nil {
			return VerifyOTPResult{}, fmt.Errorf(
				"create identity with identifier: %w",
				err,
			)
		}
	}

	return s.issueLoginTokens(
		ctx,
		challenge,
		identity,
		verifiedAt,
	)
}

func (s *service) completeIdentifierLink(
	ctx context.Context,
	challenge OTPChallenge,
	verifiedAt time.Time,
) (VerifyOTPResult, error) {
	if s.identifierLinkCompletionStore == nil {
		return VerifyOTPResult{}, errors.New(
			"identifier link completion store is not configured",
		)
	}

	if challenge.TargetIdentityID == nil {
		return VerifyOTPResult{}, errors.New(
			"link identifier OTP challenge has no target identity",
		)
	}

	identityID := strings.TrimSpace(
		*challenge.TargetIdentityID,
	)
	if identityID == "" {
		return VerifyOTPResult{}, errors.New(
			"link identifier OTP challenge target identity is blank",
		)
	}

	identifier, err := NewIdentifier(
		challenge.Identifier.Type,
		challenge.Identifier.Value,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"validate link identifier OTP identifier: %w",
			err,
		)
	}

	err = s.identifierLinkCompletionStore.Complete(
		ctx,
		IdentifierLinkCompletionInput{
			ChallengeID: challenge.ID,
			IdentityID:  identityID,
			Identifier:  identifier,
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
			ErrIdentifierAlreadyLinked,
		):
			return VerifyOTPResult{},
				ErrIdentifierAlreadyLinked

		default:
			return VerifyOTPResult{}, fmt.Errorf(
				"complete identifier link: %w",
				err,
			)
		}
	}

	return VerifyOTPResult{
		IdentityID: identityID,
	}, nil
}

func (s *service) issueLoginTokens(
	ctx context.Context,
	challenge OTPChallenge,
	identity Identity,
	verifiedAt time.Time,
) (VerifyOTPResult, error) {
	if !identity.IsActive {
		return VerifyOTPResult{},
			ErrIdentityInactive
	}

	tokenPair, err := s.tokenIssuer.Issue(
		ctx,
		TokenIssueInput{
			Identity:    identity,
			ChallengeID: challenge.ID,
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

		default:
			return VerifyOTPResult{}, fmt.Errorf(
				"issue token pair: %w",
				err,
			)
		}
	}

	return VerifyOTPResult{
		IdentityID:                  identity.ID,
		AccessToken:                 tokenPair.AccessToken,
		RefreshToken:                tokenPair.RefreshToken,
		AccessTokenExpiresInSeconds: tokenPair.AccessTokenExpiresInSeconds,
	}, nil
}

func (s *service) GetMyIdentity(
	ctx context.Context,
	input GetMyIdentityInput,
) (IdentityDetails, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return IdentityDetails{}, ErrIdentityNotFound
	}

	details, found, err := s.identityReader.FindByID(
		ctx,
		identityID,
	)
	if err != nil {
		return IdentityDetails{}, fmt.Errorf(
			"get identity details: %w",
			err,
		)
	}

	if !found {
		return IdentityDetails{}, ErrIdentityNotFound
	}

	return details, nil
}

func (s *service) RefreshToken(
	ctx context.Context,
	input RefreshTokenInput,
) (RefreshTokenResult, error) {
	if strings.TrimSpace(input.RefreshToken) == "" {
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
		s.accessTokenSigner.IssueForSession(
			refreshContext.IdentityID,
			refreshContext.SessionID,
			now,
			refreshContext.SessionExpiresAt,
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
		IdentityID:                  refreshContext.IdentityID,
		AccessToken:                 accessToken,
		RefreshToken:                replacementRefreshToken,
		AccessTokenExpiresInSeconds: accessTokenExpiresInSeconds,
	}, nil
}

func (s *service) Logout(
	ctx context.Context,
	input LogoutInput,
) error {
	if strings.TrimSpace(input.RefreshToken) == "" {
		return ErrInvalidRefreshToken
	}

	refreshTokenHash := s.refreshTokenHasher.Hash(
		input.RefreshToken,
	)

	if err := s.sessionRevocationStore.RevokeByRefreshTokenHash(
		ctx,
		refreshTokenHash,
		s.clock.Now().UTC(),
	); err != nil {
		return fmt.Errorf(
			"revoke authentication session: %w",
			err,
		)
	}

	return nil
}

func (s *service) LogoutAllSessions(
	ctx context.Context,
	input LogoutAllSessionsInput,
) error {
	if strings.TrimSpace(input.RefreshToken) == "" {
		return ErrInvalidRefreshToken
	}

	refreshTokenHash := s.refreshTokenHasher.Hash(
		input.RefreshToken,
	)

	if err := s.allSessionsRevocationStore.RevokeAllByRefreshTokenHash(
		ctx,
		refreshTokenHash,
		s.clock.Now().UTC(),
	); err != nil {
		return fmt.Errorf(
			"revoke all authentication sessions: %w",
			err,
		)
	}

	return nil
}
