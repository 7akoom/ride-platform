package auth

import (
	"testing"
	"time"
)

func TestNewServicePanicsForInvalidConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*serviceConstructorTestDependencies)
	}{
		{
			name: "nil challenge repository",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.challengeRepository = nil
			},
		},
		{
			name: "nil identity identifier repository",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identityIdentifierRepository = nil
			},
		},
		{
			name: "nil identity reader",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identityReader = nil
			},
		},
		{
			name: "nil identity lifecycle store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identityLifecycleStore = nil
			},
		},
		{
			name: "nil identifier link completion store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identifierLinkCompletionStore = nil
			},
		},
		{
			name: "nil identifier unlink request store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identifierUnlinkRequestStore = nil
			},
		},
		{
			name: "nil identifier unlink completion store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.identifierUnlinkCompletionStore = nil
			},
		},
		{
			name: "nil OTP generator",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpGenerator = nil
			},
		},
		{
			name: "nil OTP hasher",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpHasher = nil
			},
		},
		{
			name: "nil OTP delivery",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpDelivery = nil
			},
		},
		{
			name: "nil OTP request rate limiter",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimiter = nil
			},
		},
		{
			name: "nil challenge ID generator",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.challengeIDGenerator = nil
			},
		},
		{
			name: "nil token issuer",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.tokenIssuer = nil
			},
		},
		{
			name: "nil refresh token rotation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenRotationStore = nil
			},
		},
		{
			name: "nil session revocation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.sessionRevocationStore = nil
			},
		},
		{
			name: "nil all sessions revocation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.allSessionsRevocationStore = nil
			},
		},
		{
			name: "nil session reader",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.sessionReader = nil
			},
		},
		{
			name: "nil session management revocation store",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.sessionManagementRevocationStore = nil
			},
		},
		{
			name: "nil refresh token generator",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenGenerator = nil
			},
		},
		{
			name: "nil refresh token hasher",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenHasher = nil
			},
		},
		{
			name: "nil access token signer",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.accessTokenSigner = nil
			},
		},
		{
			name: "nil clock",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.clock = nil
			},
		},
		{
			name: "zero OTP TTL",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpTTL = 0
			},
		},
		{
			name: "zero OTP request cooldown",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.Cooldown = 0
			},
		},
		{
			name: "zero OTP request window",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.Window = 0
			},
		},
		{
			name: "zero OTP request max requests",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.MaxRequests = 0
			},
		},
		{
			name: "OTP cooldown exceeds window",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.otpRequestRateLimitPolicy.Cooldown =
					d.otpRequestRateLimitPolicy.Window + time.Second
			},
		},
		{
			name: "zero refresh token TTL",
			mutate: func(d *serviceConstructorTestDependencies) {
				d.refreshTokenTTL = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependencies :=
				newValidServiceConstructorTestDependencies()

			tt.mutate(&dependencies)

			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal(
						"NewServiceWithIdentityIdentifiers() did not panic for invalid configuration",
					)
				}
			}()

			newServiceFromConstructorTestDependencies(
				dependencies,
			)
		})
	}
}
