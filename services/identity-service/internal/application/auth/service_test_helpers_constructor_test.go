package auth

import "time"

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
}

type serviceConstructorTestDependencies struct {
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

	otpTTL                    time.Duration
	otpRequestRateLimitPolicy OTPRequestRateLimitPolicy
	refreshTokenTTL           time.Duration
}

func newValidServiceConstructorTestDependencies() serviceConstructorTestDependencies {
	return serviceConstructorTestDependencies{
		challengeRepository:              &testChallengeRepository{},
		identityIdentifierRepository:     &testIdentityIdentifierRepository{},
		identityReader:                   &testIdentityReader{},
		identifierLinkCompletionStore:    &testIdentifierLinkCompletionStore{},
		identifierUnlinkRequestStore:     &testIdentifierUnlinkRequestStore{},
		identifierUnlinkCompletionStore:  &testIdentifierUnlinkCompletionStore{},
		otpGenerator:                     &testOTPGenerator{},
		otpHasher:                        &testOTPHasher{},
		otpDelivery:                      &testOTPDelivery{},
		otpRequestRateLimiter:            &testOTPRequestRateLimiter{},
		challengeIDGenerator:             &testChallengeIDGenerator{},
		tokenIssuer:                      &testTokenIssuer{},
		refreshTokenRotationStore:        &testRefreshTokenRotationStore{},
		sessionRevocationStore:           &testSessionRevocationStore{},
		allSessionsRevocationStore:       &testAllSessionsRevocationStore{},
		sessionReader:                    &testSessionReader{},
		sessionManagementRevocationStore: &testSessionManagementRevocationStore{},
		refreshTokenGenerator:            &testRefreshTokenGenerator{},
		refreshTokenHasher:               &testRefreshTokenHasher{},
		accessTokenSigner:                &testAccessTokenSigner{},
		clock:                            &testClock{},

		otpTTL: 5 * time.Minute,
		otpRequestRateLimitPolicy: OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		refreshTokenTTL: 29 * 24 * time.Hour,
	}
}

func newServiceFromConstructorTestDependencies(
	dependencies serviceConstructorTestDependencies,
) Service {
	return NewServiceWithIdentityIdentifiers(
		dependencies.challengeRepository,
		dependencies.identityIdentifierRepository,
		dependencies.identityReader,
		dependencies.identifierLinkCompletionStore,
		dependencies.identifierUnlinkRequestStore,
		dependencies.identifierUnlinkCompletionStore,
		dependencies.otpGenerator,
		dependencies.otpHasher,
		dependencies.otpDelivery,
		dependencies.otpRequestRateLimiter,
		dependencies.challengeIDGenerator,
		dependencies.tokenIssuer,
		dependencies.refreshTokenRotationStore,
		dependencies.sessionRevocationStore,
		dependencies.allSessionsRevocationStore,
		dependencies.sessionReader,
		dependencies.sessionManagementRevocationStore,
		dependencies.refreshTokenGenerator,
		dependencies.refreshTokenHasher,
		dependencies.accessTokenSigner,
		dependencies.clock,
		dependencies.otpTTL,
		dependencies.otpRequestRateLimitPolicy,
		dependencies.refreshTokenTTL,
	)
}

func newIdentifierAwareServiceForTest(
	challengeRepository ChallengeRepository,
	identityIdentifierRepository IdentityIdentifierRepository,
	identityReader IdentityReader,
	identifierLinkCompletionStore IdentifierLinkCompletionStore,
	otpHasher OTPHasher,
	tokenIssuer TokenIssuer,
	clock Clock,
) Service {
	return NewServiceWithIdentityIdentifiers(
		challengeRepository,
		identityIdentifierRepository,
		identityReader,
		identifierLinkCompletionStore,
		&testIdentifierUnlinkRequestStore{},
		&testIdentifierUnlinkCompletionStore{},
		&testOTPGenerator{},
		otpHasher,
		&testOTPDelivery{},
		&testOTPRequestRateLimiter{},
		&testChallengeIDGenerator{},
		tokenIssuer,
		&testRefreshTokenRotationStore{},
		&testSessionRevocationStore{},
		&testAllSessionsRevocationStore{},
		&testSessionReader{},
		&testSessionManagementRevocationStore{},
		&testRefreshTokenGenerator{},
		&testRefreshTokenHasher{},
		&testAccessTokenSigner{},
		clock,
		5*time.Minute,
		OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
		29*24*time.Hour,
	)
}
