package auth

import "errors"

var (
	ErrChallengeNotFound         = errors.New("OTP challenge not found")
	ErrChallengeExpired          = errors.New("OTP challenge expired")
	ErrChallengeUsed             = errors.New("OTP challenge already used")
	ErrChallengeCancelled        = errors.New("OTP challenge cancelled")
	ErrChallengeAttemptsExceeded = errors.New("OTP challenge attempts exceeded")
	ErrInvalidOTP                = errors.New("invalid OTP")
	ErrIdentityInactive          = errors.New("identity is inactive")
	ErrOTPRequestRateLimited     = errors.New("OTP request rate limit exceeded")

	ErrInvalidRefreshToken = errors.New(
		"invalid refresh token",
	)
	ErrRefreshTokenExpired = errors.New(
		"refresh token expired",
	)
	ErrRefreshTokenRevoked = errors.New(
		"refresh token revoked",
	)
	ErrRefreshTokenReused = errors.New(
		"refresh token reuse detected",
	)
	ErrSessionExpired = errors.New(
		"authentication session expired",
	)
	ErrSessionRevoked = errors.New(
		"authentication session revoked",
	)
)