package auth

import "context"

type RequestOTPInput struct {
	PhoneNumber string
}

type RequestOTPResult struct {
	ChallengeID      string
	ExpiresInSeconds int32
}

type VerifyOTPInput struct {
	ChallengeID string
	Code        string
}

type VerifyOTPResult struct {
	IdentityID                  string
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type RefreshTokenInput struct {
	RefreshToken string
}

type RefreshTokenResult struct {
	IdentityID                  string
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type Service interface {
	RequestOTP(
		ctx context.Context,
		input RequestOTPInput,
	) (RequestOTPResult, error)

	VerifyOTP(
		ctx context.Context,
		input VerifyOTPInput,
	) (VerifyOTPResult, error)

	RefreshToken(
		ctx context.Context,
		input RefreshTokenInput,
	) (RefreshTokenResult, error)
}