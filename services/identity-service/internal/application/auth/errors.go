package auth

import "errors"

var (
	ErrChallengeNotFound             = errors.New("OTP challenge not found")
	ErrChallengeExpired              = errors.New("OTP challenge expired")
	ErrChallengeUsed                 = errors.New("OTP challenge already used")
	ErrOTPPurposeMismatch            = errors.New("OTP challenge purpose mismatch")
	ErrOTPChallengeTargetMismatch    = errors.New("OTP challenge target identity mismatch")
	ErrChallengeCancelled            = errors.New("OTP challenge cancelled")
	ErrChallengeAttemptsExceeded     = errors.New("OTP challenge attempts exceeded")
	ErrInvalidOTP                    = errors.New("invalid OTP")
	ErrIdentityInactive              = errors.New("identity is inactive")
	ErrOTPRequestRateLimited         = errors.New("OTP request rate limit exceeded")
	ErrOTPDeliveryChannelUnavailable = errors.New(
		"OTP delivery channel is unavailable for this identity",
	)
	ErrIdentifierAlreadyLinked = errors.New("identity identifier is already linked")
	ErrIdentifierNotLinked     = errors.New(
		"identity identifier is not linked",
	)
	ErrLastIdentifierRemoval = errors.New(
		"cannot remove the last identity identifier",
	)
	ErrIdentityNotFound = errors.New("identity not found")

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
	ErrSessionNotFound = errors.New(
		"authentication session not found",
	)
	ErrSessionExpired = errors.New(
		"authentication session expired",
	)
	ErrSessionRevoked = errors.New(
		"authentication session revoked",
	)
)
