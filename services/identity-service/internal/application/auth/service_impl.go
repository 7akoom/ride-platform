package auth

import (
	"time"
)

type service struct {
	challengeRepository              ChallengeRepository
	identityIdentifierRepository     IdentityIdentifierRepository
	identityReader                   IdentityReader
	identifierLinkCompletionStore    IdentifierLinkCompletionStore
	identifierUnlinkRequestStore     IdentifierUnlinkRequestStore
	identifierUnlinkCompletionStore  IdentifierUnlinkCompletionStore
	otpGenerator                     OTPGenerator
	otpHasher                        OTPHasher
	otpDelivery                      OTPDelivery
	otpRequestRateLimiter            OTPRequestRateLimiter
	challengeIDGenerator             ChallengeIDGenerator
	tokenIssuer                      TokenIssuer
	refreshTokenRotationStore        RefreshTokenRotationStore
	sessionRevocationStore           SessionRevocationStore
	allSessionsRevocationStore       AllSessionsRevocationStore
	sessionReader                    SessionReader
	sessionManagementRevocationStore SessionManagementRevocationStore
	refreshTokenGenerator            RefreshTokenGenerator
	refreshTokenHasher               RefreshTokenHasher
	accessTokenSigner                AccessTokenSigner
	clock                            Clock
	otpTTL                           time.Duration
	otpRequestRateLimitPolicy        OTPRequestRateLimitPolicy
	refreshTokenTTL                  time.Duration
}

var _ Service = (*service)(nil)

func NewServiceWithIdentityIdentifiers(
	challengeRepository ChallengeRepository,
	identityIdentifierRepository IdentityIdentifierRepository,
	identityReader IdentityReader,
	identifierLinkCompletionStore IdentifierLinkCompletionStore,
	identifierUnlinkRequestStore IdentifierUnlinkRequestStore,
	identifierUnlinkCompletionStore IdentifierUnlinkCompletionStore,
	otpGenerator OTPGenerator,
	otpHasher OTPHasher,
	otpDelivery OTPDelivery,
	otpRequestRateLimiter OTPRequestRateLimiter,
	challengeIDGenerator ChallengeIDGenerator,
	tokenIssuer TokenIssuer,
	refreshTokenRotationStore RefreshTokenRotationStore,
	sessionRevocationStore SessionRevocationStore,
	allSessionsRevocationStore AllSessionsRevocationStore,
	sessionReader SessionReader,
	sessionManagementRevocationStore SessionManagementRevocationStore,
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

	if identifierUnlinkRequestStore == nil {
		panic("identifier unlink request store is required")
	}

	if identifierUnlinkCompletionStore == nil {
		panic("identifier unlink completion store is required")
	}

	return newServiceWithDependencies(
		challengeRepository,
		identityIdentifierRepository,
		identityReader,
		identifierLinkCompletionStore,
		identifierUnlinkRequestStore,
		identifierUnlinkCompletionStore,
		otpGenerator,
		otpHasher,
		otpDelivery,
		otpRequestRateLimiter,
		challengeIDGenerator,
		tokenIssuer,
		refreshTokenRotationStore,
		sessionRevocationStore,
		allSessionsRevocationStore,
		sessionReader,
		sessionManagementRevocationStore,
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
	identifierUnlinkRequestStore IdentifierUnlinkRequestStore,
	identifierUnlinkCompletionStore IdentifierUnlinkCompletionStore,
	otpGenerator OTPGenerator,
	otpHasher OTPHasher,
	otpDelivery OTPDelivery,
	otpRequestRateLimiter OTPRequestRateLimiter,
	challengeIDGenerator ChallengeIDGenerator,
	tokenIssuer TokenIssuer,
	refreshTokenRotationStore RefreshTokenRotationStore,
	sessionRevocationStore SessionRevocationStore,
	allSessionsRevocationStore AllSessionsRevocationStore,
	sessionReader SessionReader,
	sessionManagementRevocationStore SessionManagementRevocationStore,
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

	if sessionReader == nil {
		panic("session reader is required")
	}

	if sessionManagementRevocationStore == nil {
		panic("session management revocation store is required")
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
		challengeRepository:              challengeRepository,
		identityIdentifierRepository:     identityIdentifierRepository,
		identityReader:                   identityReader,
		identifierLinkCompletionStore:    identifierLinkCompletionStore,
		identifierUnlinkRequestStore:     identifierUnlinkRequestStore,
		identifierUnlinkCompletionStore:  identifierUnlinkCompletionStore,
		otpGenerator:                     otpGenerator,
		otpHasher:                        otpHasher,
		otpDelivery:                      otpDelivery,
		otpRequestRateLimiter:            otpRequestRateLimiter,
		challengeIDGenerator:             challengeIDGenerator,
		tokenIssuer:                      tokenIssuer,
		refreshTokenRotationStore:        refreshTokenRotationStore,
		sessionRevocationStore:           sessionRevocationStore,
		allSessionsRevocationStore:       allSessionsRevocationStore,
		sessionReader:                    sessionReader,
		sessionManagementRevocationStore: sessionManagementRevocationStore,
		refreshTokenGenerator:            refreshTokenGenerator,
		refreshTokenHasher:               refreshTokenHasher,
		accessTokenSigner:                accessTokenSigner,
		clock:                            clock,
		otpTTL:                           otpTTL,
		otpRequestRateLimitPolicy:        otpRequestRateLimitPolicy,
		refreshTokenTTL:                  refreshTokenTTL,
	}
}
